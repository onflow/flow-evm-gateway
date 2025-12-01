# Diagnosing Missing UserOp Processing

## Current Status

- ✅ UserOp was accepted (got hash back)
- ❌ Account not created (code is empty)
- ❌ Bundler finding 0 pending UserOps

This suggests the UserOp was either:
1. Not added to pool (validation failed silently)
2. Added but immediately removed/expired
3. Processed but transaction failed

## Check What Happened

### Step 1: Check if UserOp Was Added to Pool

```bash
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|0x71ee4bc503BeDC396001C4c3206e88B965c6f860|user operation added to pool" | tail -30
```

Look for:
- `"user operation added to pool"` - Confirms it was added
- The UserOp hash or sender address

### Step 2: Check Validation Status

```bash
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|0x71ee4bc503BeDC396001C4c3206e88B965c6f860|validation.*failed|simulateValidation.*reverted" | tail -30
```

Look for:
- `"user operation validation failed"` - Validation failed
- `"EntryPoint.simulateValidation reverted"` - Simulation failed
- Any errors related to this UserOp

### Step 3: Check if Bundler Processed It

```bash
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|0x71ee4bc503BeDC396001C4c3206e88B965c6f860|created handleOps|removed UserOp from pool|submitted bundled" | tail -30
```

Look for:
- `"created handleOps transaction"` - Transaction was created
- `"removed UserOp from pool"` - UserOp was removed
- `"submitted bundled transaction"` - Transaction was submitted

### Step 4: Check for Errors

```bash
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|0x71ee4bc503BeDC396001C4c3206e88B965c6f860|error|failed|revert" | tail -30
```

## Most Likely Scenarios

### Scenario 1: Validation Failed

**Symptom**: See `"user operation validation failed"` in logs

**Cause**: EntryPoint simulation failed (signature, gas, etc.)

**Solution**: Check validation error details in logs

### Scenario 2: UserOp Expired (TTL)

**Symptom**: UserOp was added but expired before bundler processed it

**Cause**: UserOp TTL is too short, or bundler didn't run in time

**Check**: Look for TTL expiration logs

### Scenario 3: Transaction Creation Failed

**Symptom**: See `"failed to create handleOps transaction"` in logs

**Cause**: Encoding error (should be fixed now), gas estimation failed, etc.

**Solution**: Check bundler error logs

### Scenario 4: Transaction Submission Failed

**Symptom**: Transaction created but `"failed to add handleOps transaction to pool"`

**Cause**: Transaction pool rejected it

**Solution**: Check transaction pool errors

## Quick Test: Submit New UserOp with Monitoring

The best way to diagnose is to submit a fresh UserOp and watch logs in real-time:

```bash
# Terminal 1: Watch all UserOp activity
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block|ingesting|NotifyBlock" | grep -E "userop|SendUserOperation|bundler|pendingUserOpCount|created handleOps|submitted bundled|removed UserOp|validation"
```

Then submit a new UserOp and watch what happens.

## Check Recent Activity (All UserOps)

```bash
# See all UserOp activity in last 10 minutes
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -E "SendUserOperation|user operation added|validation|bundler.*pending|created handleOps|removed UserOp" | tail -50
```

This will show if ANY UserOps were processed recently.

