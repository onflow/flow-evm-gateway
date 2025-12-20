# Fix: UserOps Removed Before Transaction Submission

## Problem

UserOperations were being removed from the pool **before** confirming the transaction could be submitted to the transaction pool. This caused UserOps to be lost if `txPool.Add()` failed (e.g., "no signing keys available").

### The Bug

1. `CreateBundledTransactions()` created transaction objects
2. **UserOps were removed from pool immediately** (line 115-123 in old code)
3. `SubmitBundledTransactions()` tried to add transactions to `txPool`
4. If `txPool.Add()` failed with "no signing keys available", UserOps were already gone

### Impact

- **With short bundler interval (800ms)**: If keys aren't available, bundler keeps trying every 800ms and losing UserOps
- **UserOps are lost forever** if transaction submission fails
- **No retry mechanism** - UserOps can't be recovered

## Solution

**Only remove UserOps from pool AFTER successful transaction submission.**

### Changes

1. **New `BundledTransaction` struct**: Tracks which UserOps belong to which transaction
   ```go
   type BundledTransaction struct {
       Transaction *types.Transaction
       UserOps     []*models.UserOperation
   }
   ```

2. **Modified `CreateBundledTransactions()`**: 
   - Returns `[]*BundledTransaction` instead of `[]*types.Transaction`
   - **Does NOT remove UserOps** from pool

3. **Modified `SubmitBundledTransactions()`**:
   - Only removes UserOps after successful `txPool.Add()`
   - If `txPool.Add()` fails, UserOps remain in pool for retry

### Code Flow (After Fix)

```
1. CreateBundledTransactions()
   ├─ Create transaction objects
   ├─ Track UserOps per transaction
   └─ Return BundledTransaction[] (UserOps still in pool)

2. SubmitBundledTransactions()
   ├─ For each BundledTransaction:
   │  ├─ Try txPool.Add()
   │  ├─ If SUCCESS:
   │  │  ├─ Remove UserOps from pool ✅
   │  │  └─ Log success
   │  └─ If FAILURE:
   │     ├─ Keep UserOps in pool ✅
   │     └─ Log error (will retry on next tick)
   └─ Return
```

## Benefits

1. **UserOps are preserved** if transaction submission fails
2. **Automatic retry** - UserOps will be retried on next bundler tick (800ms)
3. **No data loss** - UserOps only removed after successful submission
4. **Better error handling** - Clear distinction between creation failure and submission failure

## Testing

All existing tests pass with the new return type. Tests updated to use `BundledTransaction` struct.

## Configuration

The bundler interval (`--bundler-interval`, default 800ms) is still configurable. With this fix:
- **Short interval (800ms)**: Faster retries if submission fails
- **Long interval (5s)**: Less frequent retries, but UserOps are preserved

## Related Issues

- **"no signing keys available"**: UserOps will now be preserved and retried when keys become available
- **Transaction pool full**: UserOps will be preserved and retried when pool has capacity
- **Network errors**: UserOps will be preserved and retried on next tick

