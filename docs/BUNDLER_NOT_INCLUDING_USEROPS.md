# Bundler Not Including UserOperations in Blocks

## Problem

UserOperations are being accepted and added to the pool, but they are never included in blocks. The bundler appears to be running but UserOps never get executed.

## Diagnostic Steps

### 1. Check if Bundler is Running

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "bundler|pendingUserOpCount|pendingCount|found pending|created bundled|submitted bundled"
```

**Expected logs:**
- `"bundler tick - checking for pending UserOperations"` - Every ~800ms
- `"pendingUserOpCount": N` - Shows how many UserOps are pending
- `"found pending UserOperations - creating bundled transactions"` - When UserOps are found
- `"created bundled transactions - submitting to transaction pool"` - When transactions are created
- `"submitted bundled transaction to pool"` - When transactions are added to pool

### 2. Check if UserOps are in Pool

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "user operation added to pool|userOpHash.*added"
```

**Expected:** Should see logs when UserOps are added to the pool.

### 3. Check for Bundler Errors

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "bundler.*error|failed.*bundl|failed.*create.*handleOps|failed.*add.*handleOps"
```

**If you see errors:**
- `"failed to create bundled transactions"` - Issue creating handleOps transactions
- `"failed to add handleOps transaction to pool"` - Issue adding to transaction pool
- `"failed to trigger bundling"` - Issue triggering bundler after UserOp added

### 4. Check Transaction Pool Activity

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "txPool|transaction.*pool|batch.*transaction"
```

**Expected:** Should see transaction pool activity when transactions are added.

## Common Issues

### Issue 1: Bundler Not Finding UserOps

**Symptom:** Logs show `"pendingUserOpCount": 0` even after adding UserOps.

**Possible Causes:**
1. UserOps are being removed from pool prematurely
2. UserOps are expiring (TTL too short)
3. Pool is not persisting UserOps correctly

**Check:**
```bash
# Check UserOp TTL setting
sudo journalctl -u flow-evm-gateway -n 100 | grep -i "user-op-ttl\|UserOpTTL"
```

### Issue 2: Transactions Created But Not Submitted

**Symptom:** Logs show `"created bundled transactions"` but no `"submitted bundled transaction"`.

**Possible Causes:**
1. `txPool.Add()` is failing silently
2. Transaction pool is rejecting transactions
3. Gas estimation is failing

**Check:**
```bash
# Look for txPool errors
sudo journalctl -u flow-evm-gateway -f | grep -iE "failed.*add.*handleOps|txPool.*error"
```

### Issue 3: Transactions Submitted But Not Included

**Symptom:** Logs show `"submitted bundled transaction to pool"` but transactions never appear in blocks.

**Possible Causes:**
1. Transaction pool is not submitting transactions to network
2. Transactions are being rejected by the network
3. Gas price is too low
4. Nonce issues

**Check:**
```bash
# Check transaction pool submission
sudo journalctl -u flow-evm-gateway -f | grep -iE "batch.*submit|transaction.*submitted.*network"
```

### Issue 4: Bundler Interval Too Long

**Symptom:** UserOps take a very long time to be processed.

**Check:**
```bash
# Check bundler interval
sudo journalctl -u flow-evm-gateway -n 100 | grep -i "bundler.*interval"
```

**Default:** 800ms (0.8 seconds)

## Enhanced Logging

The gateway now logs at Info level:
- Bundler ticks with pending count
- When UserOps are found
- When transactions are created
- When transactions are submitted
- Success/failure counts

## Next Steps

1. **Monitor logs** with the filters above
2. **Check bundler activity** - Should see ticks every ~800ms
3. **Verify UserOps in pool** - Should see pending count > 0
4. **Check transaction creation** - Should see "created bundled transactions"
5. **Verify pool submission** - Should see "submitted bundled transaction to pool"

## Log Filter for All Bundler Activity

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "bundler|user.*operation.*added|pendingUserOpCount|pendingCount|found pending|created bundled|submitted bundled|handleOps|userOpHash"
```

