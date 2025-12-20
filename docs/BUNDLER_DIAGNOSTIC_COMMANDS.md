# Bundler Diagnostic Commands

## Problem

UserOperation is accepted (hash returned) but not being included/executed. The bundler should be processing it but it's not appearing in blocks.

## Diagnostic Commands

### 1. Check if Bundler is Running

```bash
sudo journalctl -u flow-evm-gateway -n 200 --no-pager | grep -iE "bundler|handleOps|pending.*UserOperation|submitted.*bundled"
```

**What to look for:**
- `"bundler tick - checking for pending UserOperations"` - Bundler is running
- `"found pending UserOperations"` - Bundler found UserOps
- `"created handleOps transaction"` - Bundler created transaction
- `"submitted bundled transaction to pool"` - Transaction was submitted

### 2. Check for Bundler Errors

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "bundler|handleOps|failed.*create|failed.*submit|failed.*add.*pool"
```

**What to look for:**
- `"failed to create handleOps transaction"` - Error creating transaction
- `"failed to add handleOps transaction to pool"` - Error adding to tx pool
- `"failed to submit bundled transactions"` - General bundler error

### 3. Check UserOp Pool Status

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "user.*operation.*added|user.*operation.*submitted|pendingCount|removed.*UserOp.*pool"
```

**What to look for:**
- `"user operation added to pool"` - UserOp was added
- `"pendingCount"` - How many UserOps are pending
- `"removed UserOp from pool after bundling"` - UserOp was processed

### 4. Check Transaction Pool

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "txHash|transaction.*pool|handleOps"
```

**What to look for:**
- `"submitted bundled transaction to pool"` with `txHash` - Transaction was added
- Transaction hash should appear in subsequent block logs

### 5. Comprehensive Bundler Monitoring

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -E "bundler|handleOps|pendingCount|user.*operation.*(added|submitted|removed)|txHash|failed.*(create|submit|add)"
```

## Expected Flow

1. **UserOp Submitted:**
   ```
   "user operation submitted" with userOpHash
   ```

2. **Bundler Tick (every 800ms):**
   ```
   "bundler tick - checking for pending UserOperations"
   "pendingCount": 1
   "found pending UserOperations - creating bundled transactions"
   ```

3. **Transaction Created:**
   ```
   "creating handleOps transaction for batch"
   "created handleOps transaction" with txHash
   "removed UserOp from pool after bundling"
   ```

4. **Transaction Submitted:**
   ```
   "submitted bundled transaction to pool" with txHash
   ```

5. **Transaction Executed:**
   ```
   Transaction should appear in block logs
   ```

## Common Issues

### Issue 1: Bundler Not Running

**Symptom:** No "bundler tick" messages in logs

**Check:**
- Is `BUNDLER_ENABLED=true` in config?
- Check service status: `sudo systemctl status flow-evm-gateway`

### Issue 2: No Pending UserOps Found

**Symptom:** `"pendingCount": 0` even after submitting

**Possible causes:**
- UserOp was removed from pool prematurely
- UserOp TTL expired
- Pool implementation issue

### Issue 3: Transaction Creation Fails

**Symptom:** `"failed to create handleOps transaction"`

**Check logs for:**
- Gas estimation errors
- Calldata encoding errors
- EntryPoint address issues

### Issue 4: Transaction Pool Add Fails

**Symptom:** `"failed to add handleOps transaction to pool"`

**Possible causes:**
- Transaction pool full
- Invalid transaction format
- Nonce issues

## After Rebuild

With the new logging, you should see detailed bundler activity. Watch for:

1. **Bundler ticks** - Confirms bundler is running
2. **Pending count** - Shows if UserOps are in pool
3. **Transaction creation** - Shows if handleOps tx is created
4. **Transaction submission** - Shows if tx is added to pool
5. **Any errors** - Shows what's failing

This will help identify exactly where the process is breaking down.

