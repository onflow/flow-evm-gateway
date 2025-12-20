# UserOperation Account Creation Sequence

## Complete Sequence of Events

### 1. UserOp Submission (`eth_sendUserOperation`)

**Location**: `api/userop_api.go::SendUserOperation()`

**Steps**:
1. Receive UserOp from frontend
2. Convert args to `UserOperation` struct
3. **Validate UserOp** (see step 2 below)
4. If validation passes, add to pool
5. Return UserOp hash

### 2. Validation (`validator.Validate()`)

**Location**: `services/requester/userop_validator.go::Validate()`

**Steps**:
1. **Basic validation**: Check required fields, gas limits
2. **Signature validation**: 
   - For account creation: Skip off-chain validation (sender doesn't exist yet)
   - For existing accounts: Verify signature matches sender
3. **Check if account exists** (if `initCode` present):
   ```go
   accountCode, err := v.requester.GetCode(userOp.Sender, height)
   if len(accountCode) > 0 {
       // Account exists - EntryPoint will reject with AA10
       // But we continue to simulateValidation anyway
   }
   ```
4. **Call `simulateValidation`** (this is where AA13 happens):
   - Encodes `EntryPointSimulations.simulateValidation(userOp)`
   - Calls via `eth_call` on **indexed state** (not network's latest)
   - EntryPoint internally:
     - Extracts factory address from `initCode[0:20]`
     - Calls `senderCreator.createSender(initCode)`
     - Factory's `createAccount(owner, salt)` is called
     - **If factory call fails or runs OOG → AA13**
   - If validation fails, returns error (AA13, AA10, etc.)
5. **Paymaster validation** (if present)

**Key Point**: AA13 happens **during `simulateValidation`**, which is called **BEFORE** the UserOp is added to the pool.

### 3. Add to Pool

**Location**: `services/requester/userop_pool.go::Add()`

**Checks**:
1. **Duplicate by hash**: Rejects if exact same UserOp hash exists
2. **Nonce conflict**: Rejects if same sender has UserOp with same nonce
3. **Does NOT check**: If another UserOp with `initCode` for same sender exists

**Result**: UserOp added to pool, hash returned to frontend

### 4. Bundler Picks Up UserOp

**Location**: `services/requester/bundler.go::SubmitBundledTransactions()`

**Timing**: Every 800ms (configurable)

**Steps**:
1. Get pending UserOps from pool
2. Group by sender, sort by nonce
3. Create `EntryPoint.handleOps()` transactions
4. Remove UserOps from pool (after successful transaction creation)
5. Submit transactions to transaction pool

### 5. Transaction Execution

**Location**: Flow EVM execution pipeline

**Steps**:
1. Transaction wrapped in Cadence transaction
2. `EVM.run()` or `EVM.batchRun()` executes
3. EntryPoint's `handleOps()` is called
4. EntryPoint processes each UserOp:
   - If `initCode` present: Creates account via factory
   - Validates signature
   - Executes `callData`
5. Account is created (if successful)

## Would Duplicate Account Creation UserOps Cause AA13?

### Scenario: Two UserOps to Create Same Account

**UserOp 1**: `sender=0x71ee..., initCode=..., nonce=0`
**UserOp 2**: `sender=0x71ee..., initCode=..., nonce=0` (same account, different signature/hash)

### What Happens:

#### When UserOp 1 is Submitted:
1. ✅ Validation: Account doesn't exist yet → passes
2. ✅ `simulateValidation`: Factory call succeeds → passes
3. ✅ Added to pool
4. ✅ Bundler creates transaction
5. ✅ Transaction executes → Account created

#### When UserOp 2 is Submitted (after UserOp 1 is in pool but not yet executed):

**Key Question**: Does the pool check prevent this?

**Answer**: **NO** - The pool only checks:
- Duplicate hash (different signatures = different hashes) ✅ Passes
- Nonce conflict (same nonce = conflict) ❌ **Would be rejected**

**But if UserOp 2 has a different nonce** (e.g., nonce=1), it would:
1. ✅ Pass duplicate check (different hash)
2. ✅ Pass nonce check (different nonce)
3. ✅ Pass validation (account still doesn't exist in indexed state)
4. ✅ Be added to pool

**However**: When UserOp 2's `simulateValidation` runs:
- It checks the **indexed state** (not pending transactions)
- If UserOp 1 hasn't been executed yet, account still doesn't exist
- So UserOp 2 would also pass validation

**But when UserOp 2 executes**:
- If UserOp 1 already created the account → EntryPoint would reject with **AA10** (account already exists), not AA13
- If UserOp 2 executes first → It creates the account, then UserOp 1 would fail with AA10

### Scenario: UserOp 2 Submitted After UserOp 1 Executed

If UserOp 1 has already been executed and the account created:

1. ✅ UserOp 2 passes duplicate check (different hash)
2. ✅ UserOp 2 passes nonce check (if different nonce)
3. ⚠️ Validation checks account existence:
   ```go
   accountCode, err := v.requester.GetCode(userOp.Sender, height)
   if len(accountCode) > 0 {
       // Logs warning but continues to simulateValidation
   }
   ```
4. ⚠️ `simulateValidation` is called anyway
5. ❌ EntryPoint should return **AA10** (account already exists), not AA13

**But**: If the indexed state is behind (gateway is catching up), the validator might not see the account yet, so it would pass validation and get AA10 during execution instead.

## Answer to Your Question

**Would we get AA13 if there's already a UserOp to create this account in the pool?**

**Short answer**: **No, not directly.**

**Long answer**:
1. **AA13 happens during `simulateValidation`**, which runs **BEFORE** adding to pool
2. **Pool doesn't check for duplicate account creation** - only duplicate hashes and nonce conflicts
3. **If two UserOps with same nonce**: Second one is rejected by pool (nonce conflict)
4. **If two UserOps with different nonces**: Both can be in pool, but:
   - First one executes → Creates account
   - Second one executes → Gets **AA10** (account already exists), not AA13
5. **AA13 specifically means**: Factory call failed or ran OOG, not "account already exists"

**However**, there's a subtle race condition:
- If the gateway's indexed state is behind (like now, 1.1M blocks behind)
- UserOp 1 executes and creates account
- UserOp 2 is submitted before gateway indexes the account creation
- UserOp 2's validation sees account doesn't exist (stale state)
- UserOp 2 passes validation
- UserOp 2 gets added to pool
- When UserOp 2 executes, it gets AA10, not AA13

## What Could Cause AA13 Then?

AA13 means the **factory call itself failed**, not that the account exists. Possible causes:

1. **Factory `createAccount()` reverts**:
   - `require(msg.sender == senderCreator)` fails
   - Factory's EntryPoint address doesn't match
   - Factory configuration issue

2. **Factory call runs out of gas**:
   - But you have 2M verificationGasLimit, which should be plenty

3. **Factory doesn't exist or wrong address**:
   - But you confirmed factory exists and has code

4. **initCode parameters are wrong**:
   - Owner/salt encoding issue
   - Factory expects different format

5. **EntryPoint/senderCreator wiring issue**:
   - EntryPoint can't call factory via senderCreator
   - senderCreator not set correctly

## Diagnostic Steps

1. **Check if account already exists**:
   ```bash
   curl -X POST http://3.150.43.95:8545 \
     -H 'Content-Type: application/json' \
     --data '{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":["0x71ee4bc503BeDC396001C4c3206e88B965c6f860","latest"]}'
   ```

2. **Check for duplicate UserOps in pool**:
   - Look for logs showing "nonce conflict" or "duplicate user operation"

3. **Test factory call directly**:
   - Call factory's `createAccount` directly with same parameters
   - See if it succeeds or reverts

4. **Check EntryPoint/senderCreator**:
   - Verify EntryPoint's senderCreator is set correctly
   - Verify factory expects the correct EntryPoint address

