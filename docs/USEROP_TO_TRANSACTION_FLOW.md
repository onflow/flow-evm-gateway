# UserOperation to Transaction Flow

## Overview

UserOperations move from the pool to transactions through a periodic bundling process. This document explains the exact circumstances and timing.

## Flow Diagram

```
1. UserOp Submitted (eth_sendUserOperation)
   ↓
2. Validation (signature, simulation)
   ↓
3. Added to UserOp Pool
   ↓
4. [WAIT] Bundler Timer (every 800ms by default)
   ↓
5. Bundler Checks Pool (GetPending())
   ↓
6. If UserOps Found:
   ├─ Group by sender
   ├─ Sort by nonce (per sender)
   ├─ Batch (respect MaxOpsPerBundle)
   ├─ Create handleOps Transaction
   ├─ **REMOVE UserOps from Pool** ← Happens here!
   └─ Submit Transaction to TxPool
   ↓
7. Transaction Pool Processes
   ↓
8. Included in Block
```

## Detailed Flow

### 1. UserOp Submission

**Trigger**: `eth_sendUserOperation` RPC call

**Location**: `api/userop_api.go::SendUserOperation()`

**Steps**:
1. Validate UserOp (signature, simulation via EntryPointSimulations)
2. Add to pool: `userOpPool.Add()`
3. Trigger bundling (async): `triggerBundling()` - **Note**: This is a one-time trigger, not the main mechanism

**Result**: UserOp is in the pool, waiting to be bundled

### 2. Bundler Periodic Execution

**Trigger**: Timer-based (not event-driven)

**Location**: `bootstrap/bootstrap.go::StartAPIServer()`

**Configuration**:
- **Interval**: `BundlerInterval` (default: 800ms)
- **Configurable via**: `--bundler-interval` flag
- **Runs**: Continuously in a goroutine, regardless of UserOp activity

**Code**:
```go
ticker := time.NewTicker(bundlerInterval) // Default 800ms
for {
    select {
    case <-ticker.C:
        bundler.SubmitBundledTransactions(ctx)
    }
}
```

**Key Point**: The bundler runs **every 800ms** whether or not there are UserOps in the pool.

### 3. Bundler Checks Pool

**Location**: `services/requester/bundler.go::SubmitBundledTransactions()`

**Steps**:
1. Get pending UserOps: `userOpPool.GetPending()`
2. Log pending count
3. Call `CreateBundledTransactions()`

**If no UserOps found**: Returns immediately, logs "no pending UserOperations to bundle"

**If UserOps found**: Proceeds to create transactions

### 4. Transaction Creation

**Location**: `services/requester/bundler.go::CreateBundledTransactions()`

**Steps**:

#### 4.1 Grouping and Sorting
- **Group by sender**: All UserOps from the same sender are grouped together
- **Sort by nonce**: Within each sender group, UserOps are sorted by nonce (ascending)
- **Purpose**: Ensures UserOps are executed in order per sender

#### 4.2 Batching
- **Respects**: `MaxOpsPerBundle` config (default: 1)
- **Logic**: Splits UserOps into batches of `MaxOpsPerBundle` size
- **Example**: If `MaxOpsPerBundle=1` and there are 3 UserOps from sender A, creates 3 separate batches

#### 4.3 Create Transactions
For each batch:
1. **Encode calldata**: `EncodeHandleOps()` - Creates `handleOps(UserOp[], beneficiary)` calldata
2. **Estimate gas**: Calls `EstimateGas()` on EntryPoint
3. **Calculate gas price**: Uses average `maxFeePerGas` from UserOps or config default
4. **Create transaction**: `types.NewTx()` with EntryPoint address and calldata

**Critical Point**: If transaction creation **fails** (e.g., encoding error), UserOps **stay in pool** and will be retried on next bundler tick.

### 5. UserOps Removed from Pool

**Location**: `services/requester/bundler.go::CreateBundledTransactions()` (line 115-123)

**Timing**: **Immediately after successful transaction creation**, before submission to transaction pool

**Code**:
```go
// Remove UserOps from pool after creating transaction
for _, userOp := range batch {
    hash, _ := userOp.Hash(b.config.EntryPointAddress, b.config.EVMNetworkID)
    b.userOpPool.Remove(hash)
}
```

**Important**: UserOps are removed **even if transaction submission fails**. This prevents duplicates but means:
- ✅ Prevents UserOps from being included in multiple transactions
- ⚠️ If transaction submission fails, UserOps are lost (not retried)

### 6. Transaction Submission

**Location**: `services/requester/bundler.go::SubmitBundledTransactions()` (line 165-182)

**Steps**:
1. For each created transaction: `txPool.Add(ctx, tx)`
2. Transaction pool handles batching/ordering
3. Transaction is eventually included in a block

**If submission fails**: Transaction is lost, but UserOps are already removed from pool (see above)

## Conditions for UserOp → Transaction

A UserOp moves from pool to transaction when **ALL** of these conditions are met:

### Required Conditions

1. ✅ **Bundler is enabled**: `BundlerEnabled = true`
2. ✅ **Bundler timer fires**: Every 800ms (or configured interval)
3. ✅ **UserOp is in pool**: `GetPending()` returns the UserOp
4. ✅ **UserOp not expired**: TTL hasn't expired (checked by `GetPending()`)
5. ✅ **Transaction creation succeeds**: `createHandleOpsTransaction()` returns no error
6. ✅ **Encoding succeeds**: `EncodeHandleOps()` returns valid calldata

### Optional Conditions (Affect Batching)

- **Sender grouping**: UserOps from same sender are grouped together
- **Nonce ordering**: UserOps sorted by nonce within sender group
- **Batch size**: Respects `MaxOpsPerBundle` limit

## Timing

### Typical Timeline

1. **UserOp submitted**: T+0ms
2. **Next bundler tick**: T+0ms to T+800ms (average: T+400ms)
3. **Transaction created**: T+400ms + ~50-200ms (gas estimation)
4. **UserOp removed from pool**: T+450ms
5. **Transaction submitted**: T+450ms
6. **Included in block**: Depends on block time (typically seconds)

### Worst Case

- **Maximum wait**: 800ms (if UserOp submitted just after bundler tick)
- **Average wait**: 400ms (half the interval)

## Edge Cases

### Case 1: Transaction Creation Fails

**Scenario**: Encoding error, gas estimation fails, etc.

**Behavior**:
- UserOps **stay in pool**
- Bundler will retry on next tick (800ms later)
- UserOps remain until:
  - Transaction creation succeeds, OR
  - TTL expires

### Case 2: Transaction Submission Fails

**Scenario**: Transaction pool rejects transaction

**Behavior**:
- UserOps **already removed from pool** (removed after creation, before submission)
- UserOps are **lost** - not retried
- Transaction is lost

**Note**: This is a potential issue - UserOps should ideally only be removed after successful submission.

### Case 3: Multiple UserOps from Same Sender

**Scenario**: 3 UserOps from sender A with nonces 0, 1, 2

**Behavior**:
- Grouped together
- Sorted by nonce: [0, 1, 2]
- If `MaxOpsPerBundle=1`: Creates 3 separate transactions
- If `MaxOpsPerBundle=3`: Creates 1 transaction with all 3 UserOps

### Case 4: UserOp Expires (TTL)

**Scenario**: UserOp sits in pool longer than TTL

**Behavior**:
- `GetPending()` filters out expired UserOps
- Expired UserOps are not included in transactions
- Expired UserOps are removed from pool (via TTL cache)

## Configuration

### Relevant Config Flags

- `--bundler-enabled`: Enable/disable bundler (default: false)
- `--bundler-interval`: Interval between bundler ticks (default: 800ms)
- `--bundler-beneficiary`: Address to receive bundler fees (optional)
- `--max-ops-per-bundle`: Maximum UserOps per transaction (default: 1)
- `--user-op-ttl`: Time-to-live for UserOps in pool (default: configurable)

## Monitoring

### Key Logs to Watch

```bash
# Bundler is running
"bundler tick - checking for pending UserOperations"

# UserOps found
"found pending UserOperations - creating bundled transactions"

# Transaction created
"created handleOps transaction"

# UserOp removed
"removed UserOp from pool after bundling"

# Transaction submitted
"submitted bundled transaction to pool"
```

### Diagnostic Commands

```bash
# Check bundler activity
sudo journalctl -u flow-evm-gateway -f | grep -iE "bundler|pendingUserOpCount"

# Check transaction creation
sudo journalctl -u flow-evm-gateway -f | grep -iE "created handleOps|failed to create"

# Check UserOp removal
sudo journalctl -u flow-evm-gateway -f | grep -iE "removed UserOp from pool"
```

## Summary

**UserOps move from pool to transaction when**:
1. Bundler timer fires (every 800ms)
2. UserOps are found in pool (not expired)
3. Transaction creation succeeds
4. UserOps are immediately removed from pool
5. Transaction is submitted to transaction pool

**Key timing**: Average 400ms wait + ~50-200ms for transaction creation = **~450-600ms total** from submission to transaction creation.

**Critical point**: UserOps are removed **after transaction creation, before submission**. If submission fails, UserOps are lost.

