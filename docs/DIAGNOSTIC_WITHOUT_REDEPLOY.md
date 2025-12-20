# Diagnostic Commands - No Redeploy Required

These commands can be run on the EC2 instance right now to diagnose why UserOps aren't being included.

## 1. Check if Bundler is Enabled

```bash
# Check service file for bundler flag
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep -i bundler

# Check environment file
sudo cat /etc/flow/runtime-conf.env | grep -i BUNDLER

# Check startup logs for bundler initialization
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -i "bundler\|BundlerEnabled" | head -20
```

**Expected:**
- Service file should have `--bundler-enabled=${BUNDLER_ENABLED}` or `--bundler-enabled=true`
- Environment file should have `BUNDLER_ENABLED=true`
- Startup logs should show bundler being initialized

## 2. Check Bundler Configuration

```bash
# Check bundler interval
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -i "bundler.*interval\|BundlerInterval" | head -5

# Check max ops per bundle
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -i "max.*ops.*bundle\|MaxOpsPerBundle" | head -5
```

**Expected:**
- Bundler interval should be set (default 800ms)
- MaxOpsPerBundle should be set (default 1)

## 3. Check if Bundler is Running (Raw Logs - No Filter)

```bash
# Check ALL logs for bundler activity (including Debug level)
sudo journalctl -u flow-evm-gateway -f --no-pager | grep -i bundler
```

**What to look for:**
- `"bundler tick"` - Shows bundler is running
- `"checking for pending UserOperations"` - Shows bundler is checking
- `"no pending UserOperations"` - Shows bundler found nothing
- `"found pending UserOperations"` - Shows bundler found UserOps
- `"created bundled transactions"` - Shows transactions were created
- `"submitted bundled transaction"` - Shows transactions were submitted

## 4. Check UserOp Pool Activity

```bash
# Check if UserOps are being added to pool
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "user.*operation.*added|user.*operation.*submitted|userOpHash.*added" | tail -20

# Check for UserOp pool errors
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "pool.*error|failed.*add.*pool|userOp.*pool.*fail" | tail -20
```

**Expected:**
- Should see logs when UserOps are added: `"user operation submitted"` or `"user operation added"`
- Should NOT see pool errors

## 5. Check for Bundler Errors

```bash
# Check for bundler creation/execution errors
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "bundler.*error|failed.*bundl|failed.*create.*handleOps|failed.*add.*handleOps|failed.*trigger.*bundl" | tail -20
```

**If you see errors:**
- `"failed to create bundled transactions"` - Issue creating handleOps transactions
- `"failed to add handleOps transaction to pool"` - Issue adding to transaction pool
- `"failed to trigger bundling"` - Issue triggering bundler

## 6. Check Transaction Pool Activity

```bash
# Check if transactions are being added to pool
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "txPool|transaction.*pool|batch.*transaction|handleOps.*transaction" | tail -20

# Check for transaction pool errors
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "pool.*error|failed.*pool|transaction.*fail" | tail -20
```

**Expected:**
- Should see transaction pool activity when handleOps transactions are added
- Should NOT see pool errors

## 7. Check Recent UserOp Activity

```bash
# See all UserOp-related logs from last hour
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "userOp|sendUserOperation|eth_sendUserOperation" | tail -30
```

**What to look for:**
- UserOp requests received
- UserOp validation results
- UserOp added to pool
- UserOp hash returned

## 8. Check Service Status and Configuration

```bash
# Check service status
sudo systemctl status flow-evm-gateway --no-pager

# Check if service is running
sudo systemctl is-active flow-evm-gateway

# Check service file for all flags
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep -E "entry-point|bundler" -A 2 -B 2
```

## 9. Real-Time Monitoring (Run This While Testing)

```bash
# Monitor ALL bundler and UserOp activity in real-time
sudo journalctl -u flow-evm-gateway -f | grep -iE "bundler|userOp|sendUserOperation|handleOps|pending|pool"
```

**Then send a UserOp and watch for:**
1. UserOp received
2. UserOp validated
3. UserOp added to pool
4. Bundler tick (should happen within 1 second)
5. Bundler finding UserOps
6. Transactions created
7. Transactions submitted

## 10. Check if Bundler Goroutine Started

```bash
# Check startup logs for bundler initialization
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "start.*bundl|bundler.*start|periodic.*bundl" | head -10
```

**Expected:**
- Should see bundler being started in bootstrap logs
- Should see periodic bundling goroutine started

## 11. Check EntryPoint Configuration

```bash
# Verify EntryPoint address is configured
sudo cat /etc/flow/runtime-conf.env | grep -i ENTRY_POINT

# Check logs for EntryPoint configuration
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "entryPoint|EntryPoint" | head -10
```

**Expected:**
- `ENTRY_POINT_ADDRESS` should be set
- `ENTRY_POINT_SIMULATIONS_ADDRESS` should be set
- Logs should show EntryPoint addresses

## 12. Check for Silent Failures

```bash
# Check for any errors in the last hour
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "error|fail|panic" | grep -vE "new evm block|ingesting" | tail -30
```

## Quick Diagnostic Script

Run this to get a comprehensive view:

```bash
echo "=== Service Status ==="
sudo systemctl status flow-evm-gateway --no-pager | head -10

echo -e "\n=== Bundler Configuration ==="
sudo cat /etc/flow/runtime-conf.env | grep -i BUNDLER
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep -i bundler

echo -e "\n=== Recent Bundler Activity (last 50 lines) ==="
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -i bundler | tail -50

echo -e "\n=== Recent UserOp Activity (last 30 lines) ==="
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "userOp|sendUserOperation" | tail -30

echo -e "\n=== Recent Errors (last 20 lines) ==="
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "error|fail" | grep -vE "new evm block|ingesting" | tail -20
```

## What Each Check Tells You

1. **Bundler Enabled?** → If not, that's the problem
2. **Bundler Running?** → If no ticks, bundler isn't running
3. **UserOps in Pool?** → If no "added to pool" logs, UserOps aren't being accepted
4. **Bundler Finding UserOps?** → If "no pending" but UserOps were added, pool issue
5. **Transactions Created?** → If no "created bundled", bundler logic issue
6. **Transactions Submitted?** → If no "submitted", txPool.Add() failing
7. **Errors?** → Any errors will point to the issue

## Most Likely Issues

Based on symptoms (UserOps accepted but never included):

1. **Bundler not running** - Check #1, #2, #10
2. **Bundler not finding UserOps** - Check #3, #4 (pool issue)
3. **Transactions not being created** - Check #5 (bundler logic issue)
4. **Transactions not being submitted** - Check #6 (txPool issue)
5. **Transactions submitted but not included** - Check #6 (network/pool issue)

Run these commands and share the output to identify the exact issue!

