# Critical Fix: Database Persistence Issue

## Problem

**The gateway loses all indexed blocks (1+ million blocks) on every restart.**

## Root Cause

The systemd service file (`deploy/systemd-docker/flow-evm-gateway.service`) had **`--force-start-height` set**, which was resetting the database height on every restart.

### What Was Happening:

1. Gateway indexes blocks → Stores in PebbleDB at `/data` with current height
2. Gateway restarts → `--force-start-height` flag is set to a value (e.g., initial height)
3. Gateway startup code sees `ForceStartCadenceHeight != 0` → **Overwrites database height** with forced value
4. Gateway resumes from the forced height → **Ignores actual database progress**

### The Problematic Flag:

The service file had:

```ini
ExecStart=docker run \
	--name flow-evm-gateway \
	-v /data/evm-gateway:/data \
	... \
    --force-start-height=${FORCE_START_HEIGHT} \
```

**Problem**: `--force-start-height` is designed for **force reindexing** only. When set, it overwrites the database's stored height on **every startup**, causing the gateway to ignore its actual progress.

## The Fix

**Removed `--force-start-height` flag** to allow the gateway to read the actual database height:

```ini
ExecStart=docker run \
	--name flow-evm-gateway \
	-v /data/evm-gateway:/data \
	... \
    # --force-start-height removed - gateway now reads from database
```

### Additional Improvements:

1. **Removed `--rm` flag** - Prevents container removal from interrupting database writes
2. **Added graceful shutdown** - Ensures database flushes before termination:
   ```ini
   ExecStop=/bin/bash -c 'docker stop --time=30 flow-evm-gateway 2>/dev/null || true'
   ExecStopPost=/bin/bash -c 'docker rm -f flow-evm-gateway 2>/dev/null || true'
   ```
3. **Added directory creation** - Ensures database directory exists with proper permissions

## Deployment Steps

### 1. Create Database Directory on Host

```bash
sudo mkdir -p /data/evm-gateway
sudo chown $USER:$USER /data/evm-gateway  # Or appropriate user
```

### 2. Update Service File

The service file has been updated. Copy it to your server:

```bash
sudo cp deploy/systemd-docker/flow-evm-gateway.service /etc/systemd/system/flow-evm-gateway.service
```

### 3. Reload and Restart

```bash
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
```

### 4. Verify Database Persistence

```bash
# Check database directory exists and has files
ls -lah /data/evm-gateway/

# Check gateway logs for startup height
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -i "start-cadence-height\|latest-cadence-height"
```

**Expected**: Gateway should resume from the last indexed height, not start from beginning.

## Impact

**Before Fix**:

- ❌ Gateway loses all progress on every restart
- ❌ Must re-index millions of blocks each time
- ❌ Takes days/weeks to catch up

**After Fix**:

- ✅ Database persists across restarts
- ✅ Gateway resumes from last indexed height
- ✅ Only needs to catch up blocks created while gateway was down

## Verification

After deploying the fix, check logs:

```bash
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -E "start-cadence-height|latest-cadence-height|missed-heights"
```

You should see:

```
start-cadence-height: <last indexed height>
latest-cadence-height: <network height>
missed-heights: <small number, not 1M+>
```

If `missed-heights` is still 1M+, the database directory might not exist or have wrong permissions.

## Important Notes

1. **`--force-start-height` usage**: This flag should **only** be used for:

   - Initial database setup
   - Force reindexing from a specific height
   - Testing/debugging
   - **Never in normal production operation**

2. **Database location**: `/data/evm-gateway` on host → `/data` in container (volume mount required)

3. **Backup**: Consider backing up `/data/evm-gateway` periodically

4. **Disk space**: Ensure `/data` has sufficient space (database grows over time)

5. **Graceful shutdown**: The `ExecStop` commands ensure database writes are flushed before container termination
