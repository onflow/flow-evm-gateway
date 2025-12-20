# AA13 Error Diagnosis and Solution

## What is AA13?

**AA13 "initCode failed or OOG"** is an ERC-4337 EntryPoint error code that means:
- The `initCode` execution **ran out of gas** (OOG), OR
- The `initCode` execution **reverted** during account creation

## Current Error Details

From your logs:
```
"aaErrorCode":"AA13"
"decodedResult":"FailedOp(opIndex=0, reason=\"AA13 initCode failed or OOG\")"
```

**UserOperation details:**
- Factory: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` ✅ (exists, has code)
- Account: `0x71ee4bc503BeDC396001C4c3206e88B965c6f860` ✅ (doesn't exist yet)
- `verificationGasLimit`: 2,000,000 (2M)
- `callGasLimit`: 500,000 (500K)
- `preVerificationGas`: 50,000

## Root Cause

**Most likely: Insufficient gas for account creation**

Account creation via `SimpleAccountFactory.createAccount()` involves:
1. Factory contract call (~21k gas)
2. CREATE2 address calculation
3. Proxy contract deployment via CREATE2 (~100k-200k gas)
4. Account initialization (~50k-100k gas)
5. Storage writes (owner, entryPoint, etc.)

**Total gas requirement: Typically 2.5M-3.5M gas**

Your current `verificationGasLimit` of 2M is likely insufficient.

## Solution

### Step 1: Increase Gas Limits

Update your frontend/client to use higher gas limits:

```json
{
  "verificationGasLimit": "0x2dc6c0",  // 3,000,000 (3M) - minimum recommended
  "callGasLimit": "0x7a1200",          // 8,000,000 (8M) - for account operations
  "preVerificationGas": "0xc350"       // 50,000 - keep same
}
```

**If 3M still fails, try 4M:**
```json
{
  "verificationGasLimit": "0x3d0900"  // 4,000,000 (4M)
}
```

### Step 2: Verify Factory Setup

The factory must be correctly configured. Verify:
- Factory address is correct: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- Factory has code deployed (confirmed ✅)
- Factory is configured with correct EntryPoint address

### Step 3: Check Account Funding

If not using a paymaster, ensure the account has sufficient native tokens to:
- Pay for account creation gas
- Pay for any prefund requirements

## Why This Is Common

AA13 is one of the most common errors in ERC-4337 account creation because:

1. **Gas estimation is difficult**: Account creation gas varies based on:
   - Factory implementation
   - Proxy deployment method (CREATE vs CREATE2)
   - Account initialization complexity
   - Network gas costs

2. **Frontend defaults are often too low**: Many clients default to 1M-2M gas, which is insufficient for CREATE2 deployments.

3. **No standard gas limits**: Unlike regular transactions, there's no standard "account creation gas limit" - it depends on the factory.

## Gateway Status

✅ **Gateway is working correctly:**
- initCode is correctly formatted and passed to EntryPoint
- Signature validation is working
- EntryPoint contract is found and called
- Error decoding is working (AA13 is correctly identified)

The issue is **client-side gas limits**, not the gateway.

## Testing After Fix

After increasing gas limits, you should see:
- ✅ `simulateValidation` succeeds (returns `ValidationResult`)
- ✅ UserOperation is accepted by the gateway
- ✅ Account is created successfully

If AA13 persists after increasing to 4M gas, the issue may be:
- Factory implementation bug
- EntryPoint/senderCreator setup issue
- Network-specific gas cost differences

## References

- [Alchemy: How to Resolve EntryPoint AAxx Errors](https://www.alchemy.com/support/how-to-resolve-entrypoint-aaxx-errors)
- [Pimlico: EntryPoint Errors - AA13](https://docs.pimlico.io/infra/bundler/entrypoint-errors/aa13)
- [Biconomy: Common Errors - AA13](https://legacy-docs.biconomy.io/3.0/troubleshooting/commonerrors)

