# Block Indexing Lag Issue

## Problem

The gateway is **1,133,894 blocks behind** the network:

- Gateway: `81,178,590`
- Network: `82,312,484`
- Difference: `1,133,894 blocks`

This causes:

- ❌ Stale nonce values
- ❌ Stale state data
- ❌ Transaction failures
- ❌ Poor user experience

## Root Cause: **Gateway Was Down/Stopped**

### **Most Likely: Gateway Was Stopped for Extended Period**

If the gateway was stopped or crashed for a period of time, it will be far behind when it restarts. The gateway processes blocks sequentially, so catching up takes time.

**Common Scenarios:**

1. **Gateway was restarted** after being down for days/weeks
2. **Gateway crashed** due to an error and wasn't restarted immediately
3. **Deployment issue** - gateway was stopped during deployment
4. **Resource exhaustion** - gateway was killed due to OOM or CPU limits

### **Secondary: Resource Constraints**

This specific deployment might have resource constraints that slow down processing:

- **CPU**: Limited CPU cores slow down transaction replay
- **Memory**: Insufficient RAM causes swapping/GC pressure
- **Disk I/O**: Slow disk (especially if using network storage) slows down database writes
- **Network**: High latency to Flow access nodes slows down event subscription

### 2. **Fatal Error Handling**

The ingestion engine **stops completely** if any block indexing fails:

```go
// services/ingestion/engine.go:148-152
err := e.processEvents(events.Events)
if err != nil {
    e.log.Error().Err(err).Msg("failed to process EVM events")
    return err  // <-- Engine stops here!
}
```

**Impact**: If a single block fails to index (due to network issues, database errors, etc.), the entire indexing process stops and doesn't restart automatically.

### 3. **Sequential Processing**

Blocks are processed **one at a time, sequentially**:

```go
// Each block requires:
1. Replay all transactions (~100-500ms per block)
2. Store state changes (~50-200ms)
3. Index transactions (~10-50ms per tx)
4. Index receipts (~10-50ms per receipt)
5. Index traces (~50-200ms per tx)  // <-- BOTTLENECK!
6. Index UserOp events (if enabled)
```

**Impact**: If processing is slower than block production, the gateway falls behind.

### 3. **No Automatic Catch-Up**

When the gateway restarts, it resumes from the last indexed height, but:

- There's no fast catch-up mechanism
- It processes blocks at the same slow rate
- If it's far behind, it may never catch up

### 4. **Heavy Processing Per Block**

Each block requires significant computation:

- Transaction replay (CPU intensive)
- State storage (I/O intensive)
- Trace collection (CPU + I/O intensive)

**Impact**: On slower hardware or under load, processing can't keep up with network block production.

## Why This Happens

1. **Gateway Restart**: If the gateway was restarted, it resumes from the last indexed height. If it was down for a while, it has a large gap to fill.

2. **Indexing Error**: A single block indexing error stops the entire engine. The gateway needs to be manually restarted.

3. **Resource Constraints**: CPU, memory, or disk I/O bottlenecks slow down processing.

4. **Network Issues**: Connection problems with Flow access nodes can cause disconnections, which stop indexing.

## Solutions

### Immediate Fix: Restart Gateway

If the gateway is stopped due to an error, restart it:

```bash
sudo systemctl restart flow-evm-gateway
```

The gateway will resume from the last indexed height and start catching up.

### **Primary Solution: Make Trace Collection Optional**

**Problem**: Trace collection is the bottleneck, but it's only needed for `debug_trace*` APIs.

**Solution**: Make trace collection optional or on-demand:

1. **Option A: Disable Trace Collection** (if `debug_trace*` APIs aren't needed)

   - Use `NopTracer` instead of `CallTracerCollector`
   - This will significantly speed up block indexing

2. **Option B: Lazy Trace Collection** (collect traces only when requested)

   - Don't collect traces during block indexing
   - Collect traces on-demand when `debug_trace*` APIs are called
   - Requires re-executing transactions, but only when needed

3. **Option C: Fast Catch-Up Mode**
   - When far behind, skip trace collection
   - Only collect traces when caught up

### Long-Term Solutions

#### 1. **Add Error Recovery**

Modify `services/ingestion/engine.go` to retry failed blocks instead of stopping:

```go
err := e.processEvents(events.Events)
if err != nil {
    e.log.Error().Err(err).Msg("failed to process EVM events")
    // TODO: Add retry logic or skip block instead of stopping
    // For now, continue to next block
    continue  // Don't stop the engine
}
```

#### 2. **Optimize Trace Collection**

- Only collect traces for blocks with transactions (skip empty blocks)
- Batch trace storage operations
- Use async trace collection (don't block block indexing)

#### 3. **Monitor and Alert**

Add metrics to track:

- Blocks behind
- Indexing rate (blocks/second)
- Trace collection time per block
- Error rate
- Processing time per block

Alert when the gateway falls behind or stops indexing.

## Diagnostic Commands

### Check if Gateway is Processing Blocks

```bash
# Check if blocks are being indexed (should see new blocks every few seconds)
sudo journalctl -u flow-evm-gateway -f | grep "new evm block executed event"

# Count blocks indexed in last hour
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep "new evm block executed event" | wc -l
```

**Expected**: Should see blocks being indexed regularly. If no blocks, the gateway may be stopped or stuck.

### Check for Errors That Stopped Indexing

```bash
# Look for fatal errors that stopped the engine
sudo journalctl -u flow-evm-gateway --since "24 hours ago" | grep -E "failed to process EVM events|failed to index|failed to replay|engine.*stopped"

# Check for crashes or panics
sudo journalctl -u flow-evm-gateway --since "24 hours ago" | grep -iE "panic|fatal|crash|killed"
```

### Check Resource Usage

```bash
# Check CPU and memory usage
sudo docker stats flow-evm-gateway --no-stream

# Check disk I/O
sudo iotop -o -d 1 | grep -i docker

# Check if disk is full
df -h
```

### Check When Gateway Last Started

```bash
# Check gateway startup time
sudo journalctl -u flow-evm-gateway | grep -i "starting\|started" | tail -5

# Check systemd service status
sudo systemctl status flow-evm-gateway
```

## Expected Behavior

- **Normal**: Gateway indexes blocks in real-time, staying within 10-100 blocks of the network
- **Catching Up**: After restart, gateway processes blocks as fast as possible to catch up
- **Error**: If indexing fails, gateway should retry or skip the block, not stop completely

## Current Status

The gateway needs to:

1. ✅ Resume indexing from block `81,178,590`
2. ✅ Process `1,133,894` blocks to catch up
3. ⚠️ **Normal processing rate**: ~1-5 blocks/second (depends on block complexity)
4. ⚠️ **Catch-up time**: At 1 block/second = **~13 days**, at 5 blocks/second = **~2.6 days**

**Key Question**: Is the gateway currently processing blocks, or is it stopped?

- **If processing**: It will catch up eventually, but may take days
- **If stopped**: Need to identify why it stopped and restart it

## Recommendations

1. **Immediate**: Restart the gateway to resume indexing
2. **Short-term**: Monitor logs to identify why indexing stopped
3. **Long-term**: Implement error recovery and fast catch-up mode
4. **Monitoring**: Add alerts for indexing lag and errors
