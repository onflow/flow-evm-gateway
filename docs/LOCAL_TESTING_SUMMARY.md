# Local Testing Summary - Bundler Height Fix

## What Was Fixed

The bundler was using `requester.GetLatestEVMHeight()` (network's latest height) instead of `blocks.LatestEVMHeight()` (indexed height), causing "entity not found" errors when calling `EntryPoint.getUserOpHash()`.

## Changes Made

1. **Added `blocks storage.BlockIndexer` to Bundler struct** - Allows bundler to access indexed height
2. **Updated all height calls in bundler** - Changed from `GetLatestEVMHeight()` to `blocks.LatestEVMHeight()`
3. **Updated bootstrap** - Passes `blocks` to `NewBundler()`
4. **Updated all tests** - Pass `mockBlocks` to `NewBundler()`

## Local Testing Results

### ✅ All Tests Pass

```bash
$ go test ./services/requester -v
PASS
ok      github.com/onflow/flow-evm-gateway/services/requester   0.869s
```

### ✅ New Test: `TestBundler_UsesIndexedHeight`

This test verifies that:
- ✅ Bundler calls `blocks.LatestEVMHeight()` (indexed height)
- ✅ Bundler does NOT call `requester.GetLatestEVMHeight()` (network latest)
- ✅ Bundler successfully calls `GetUserOpHash()` with the indexed height

**Test Result:**
```
=== RUN   TestBundler_UsesIndexedHeight
--- PASS: TestBundler_UsesIndexedHeight (0.00s)
PASS
```

### ✅ Build Verification

```bash
$ go build ./...
# No errors - all packages compile successfully
```

## What This Fixes

**Before:**
- Bundler used network's latest height → EntryPoint contract not available → "entity not found" error
- `GetUserOpHash()` failed → Bundler couldn't create transactions → UserOps stuck in pool

**After:**
- Bundler uses indexed height → EntryPoint contract available → `GetUserOpHash()` succeeds
- Bundler can create transactions → UserOps get bundled and submitted

## Verification After Deployment

After redeploying, you should see:

1. **No more "entity not found" errors:**
   ```bash
   sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
     grep -i "entity not found" | wc -l
   # Should return 0
   ```

2. **Successful `getUserOpHash` calls:**
   ```bash
   sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
     grep -i "userOpHash_from_contract"
   # Should show hash values from EntryPoint.getUserOpHash()
   ```

3. **Bundler successfully creating transactions:**
   ```bash
   sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
     grep -iE "created handleOps|submitted.*transaction"
   # Should show successful transaction creation and submission
   ```

4. **No hash mismatch errors:**
   - Frontend and gateway should now calculate the same hash
   - Both use `EntryPoint.getUserOpHash()` with indexed height

## Files Changed

- `services/requester/bundler.go` - Added blocks field, updated height calls
- `bootstrap/bootstrap.go` - Pass blocks to NewBundler
- `services/requester/bundler_test.go` - Updated all test calls
- `services/requester/bundler_height_test.go` - New test to verify fix

## Next Steps

1. ✅ **Local testing complete** - All tests pass
2. **Rebuild Docker image** - Use the rebuild instructions
3. **Redeploy to EC2** - Follow redeploy instructions
4. **Monitor logs** - Verify no more "entity not found" errors
5. **Test UserOp submission** - Verify hash matches frontend
