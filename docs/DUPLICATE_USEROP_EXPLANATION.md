# Duplicate UserOp Error Explanation

## Current Situation

You're seeing "duplicate user operation" errors because:

1. **UserOps were accepted** - Validation passed, UserOps were added to the pool ✅
2. **Bundler failed to create transactions** - The ABI encoding bug prevented transaction creation ❌
3. **UserOps remained in pool** - Since transactions weren't created, UserOps weren't removed
4. **Resubmission rejected** - When you try to resubmit the same UserOp, it's rejected as a duplicate

## Why This Happens

The bundler removes UserOps from the pool **immediately after successfully creating a transaction** (not after submission). This is by design to prevent UserOps from being included in multiple transactions.

However, if transaction creation fails (due to the encoding bug), UserOps stay in the pool.

## Solution

### Option 1: Deploy the Fix (Recommended)

Once the `testnet-v1-fix-handleops-encoding` version is deployed:

1. The bundler will successfully create transactions
2. Existing UserOps in the pool will be processed
3. UserOps will be removed from the pool after being bundled
4. New UserOps can be submitted normally

### Option 2: Wait for TTL Expiration

UserOps have a TTL (Time To Live) configured in the gateway. After the TTL expires, stale UserOps are automatically removed from the pool.

### Option 3: Use a Different Nonce

If you need to test immediately, you can:
- Use a different nonce for the same sender
- Or wait for the existing UserOp to be processed after deployment

## What to Expect After Deployment

1. **Bundler will process existing UserOps** - The bundler runs every 800ms and will pick up the pending UserOps
2. **Transactions will be created** - The encoding fix allows transactions to be created successfully
3. **UserOps will be removed** - After bundling, UserOps are removed from the pool
4. **New UserOps can be submitted** - Once the pool is cleared, new UserOps can be submitted

## Monitoring After Deployment

Watch for these logs to confirm the fix is working:

```bash
# Should see successful transaction creation
sudo journalctl -u flow-evm-gateway -f | grep -E "created handleOps transaction|submitted bundled transaction"

# Should see UserOps being removed
sudo journalctl -u flow-evm-gateway -f | grep -E "removed UserOp from pool"

# Should NOT see encoding errors anymore
sudo journalctl -u flow-evm-gateway -f | grep -iE "failed to encode|encoding"
```

## Expected Timeline

- **Before deployment**: UserOps accumulate in pool, duplicates rejected
- **After deployment**: Bundler processes existing UserOps within ~1 second (next bundler tick)
- **After processing**: Pool is cleared, new UserOps can be submitted

## Note

The duplicate error is actually a **good sign** - it means:
- ✅ UserOps are being accepted and validated correctly
- ✅ The pool is working correctly (preventing duplicates)
- ✅ The only issue was the encoding bug preventing transaction creation

Once the fix is deployed, everything should work end-to-end.

