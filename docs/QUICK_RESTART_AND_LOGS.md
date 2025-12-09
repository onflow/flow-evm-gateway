# Quick Restart and Log Monitoring Commands

## On EC2 Instance (SSH in first)

### 1. Restart Service

```bash
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
sudo systemctl status flow-evm-gateway --no-pager
```

### 2. Monitor Logs in Real-Time (Filtered for Key Events)

```bash
# Watch for UserOp activity, getUserOpHash calls, and errors
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|getUserOpHash|userOpHash_from_contract|entity not found|failed to get UserOp hash|bundler|AA24"
```

### 3. Check Last 5 Minutes - Verify Fix is Working

```bash
# Check for successful getUserOpHash calls (should see "userOpHash_from_contract")
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -i "userOpHash_from_contract" | \
  tail -10

# Check for "entity not found" errors (should be ZERO after fix)
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -i "entity not found" | \
  wc -l
# Expected: 0

# Check bundler activity - should see successful transaction creation
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "created handleOps|submitted.*transaction|bundler.*success" | \
  tail -10
```

### 4. Check Last Submitted UserOp (After Submitting One)

```bash
# Comprehensive UserOp submission check
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | \
  grep -iE "received eth_sendUserOperation|userOpHash_from_contract|user operation added to pool|created handleOps|submitted.*transaction|AA24" | \
  tail -20
```

### 5. Verify No More "Entity Not Found" Errors

```bash
# Should return nothing if fix is working
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | \
  grep -iE "entity not found|failed to call getUserOpHash.*entity"
```

### 6. Check Bundler is Using Indexed Height

```bash
# Look for bundler height logs (should use indexed height, not network latest)
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "bundler.*height|got indexed EVM height|got latest EVM height" | \
  tail -10
```

## Expected Behavior After Fix

### ✅ What You Should See:

1. **`"userOpHash_from_contract"` logs** - Hash values from EntryPoint.getUserOpHash()
2. **No "entity not found" errors** - Bundler successfully calls getUserOpHash
3. **Successful bundler activity** - "created handleOps transaction" and "submitted bundled transaction"
4. **No hash mismatch warnings** - Frontend and gateway hashes match

### ❌ What You Should NOT See:

1. ❌ `"failed to call getUserOpHash: entity not found"`
2. ❌ `"failed to get UserOp hash from EntryPoint.getUserOpHash()"`
3. ❌ `"entity not found"` errors in bundler logs

## Quick Test: Submit a UserOp and Watch

```bash
# Terminal 1: Watch logs in real-time
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|getUserOpHash|bundler|entity not found"

# Terminal 2: Submit a UserOp from your frontend
# Then watch Terminal 1 for:
# 1. "received eth_sendUserOperation request"
# 2. "userOpHash_from_contract" (hash from EntryPoint)
# 3. "user operation added to pool"
# 4. "created handleOps transaction"
# 5. "submitted bundled transaction"
# 6. NO "entity not found" errors
```

## Troubleshooting

### If still seeing "entity not found":

```bash
# Check what height bundler is using
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "bundler.*height|got.*EVM height" | \
  tail -5

# Check if blocks.LatestEVMHeight() is being called
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -i "got indexed EVM height"
# Should see this message if fix is working
```

### If getUserOpHash is still failing:

```bash
# Check EntryPoint address is correct
sudo cat /etc/flow/runtime-conf.env | grep ENTRY_POINT

# Check if EntryPoint contract exists at indexed height
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "entryPoint|EntryPoint address" | \
  tail -5
```

