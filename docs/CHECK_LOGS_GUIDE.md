# How to Check Gateway Logs for UserOperation Validation

## Quick Check (No Filtering)

First, check if ANY logs are being generated when you submit a UserOp:

```bash
# Watch all logs in real-time (no filtering)
sudo journalctl -u flow-evm-gateway -f
```

Then submit your UserOp from the frontend. You should see logs with:
- `"component":"userop-api"` - when request is received
- `"component":"userop-validator"` - during validation
- `"endpoint":"SendUserOperation"` - API endpoint logs

## Filtered Check (Recommended)

Use these filters to see UserOperation-related logs:

### Option 1: Simple Filter (Recommended)
```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|validation|simulation|signature|revert|error"
```

### Option 2: Component-Based Filter
```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "userop-api|userop-validator"
```

### Option 3: JSON Field Filter
```bash
# Filter by component field in JSON logs
sudo journalctl -u flow-evm-gateway -f | grep -E '"component":"userop'
```

### Option 4: Check Last 100 Lines
```bash
# Check recent logs without following
sudo journalctl -u flow-evm-gateway -n 100 --no-pager | grep -iE "userop|validation|simulation"
```

## What to Look For

### 1. Request Received
Look for:
```json
{
  "level": "info",
  "component": "userop-api",
  "endpoint": "SendUserOperation",
  "sender": "0x...",
  "message": "received eth_sendUserOperation request"
}
```

### 2. Pre-Validation Logs
Look for:
```json
{
  "level": "info",
  "component": "userop-validator",
  "message": "calling EntryPoint.simulateValidation with full UserOp details",
  "userOpHash": "0x...",
  "ownerFromInitCode": "0x...",
  "recoveredSigner": "0x...",
  "signerMatchesOwner": true/false
}
```

### 3. Validation Errors
Look for:
```json
{
  "level": "error",
  "component": "userop-validator",
  "message": "EntryPoint.simulateValidation reverted",
  "revertReasonHex": "0x...",
  "decodedRevertReason": "..."
}
```

### 4. API Errors
Look for:
```json
{
  "level": "error",
  "component": "userop-api",
  "message": "user operation validation failed"
}
```

## If No Logs Appear

### Check 1: Is the service running?
```bash
sudo systemctl status flow-evm-gateway
```

### Check 2: Check raw logs without filtering
```bash
sudo journalctl -u flow-evm-gateway -n 50 --no-pager
```

### Check 3: Check if request is reaching the gateway
```bash
# Monitor all logs in real-time
sudo journalctl -u flow-evm-gateway -f
# Then submit UserOp - you should see SOMETHING
```

### Check 4: Verify the version has the logging
```bash
sudo journalctl -u flow-evm-gateway -n 10 --no-pager | grep version
# Should show: "version":"testnet-v1-enhanced-logging"
```

### Check 5: Check Docker logs directly
```bash
docker logs flow-evm-gateway --tail 50 -f
```

## Common Issues

### Issue: No logs at all
**Possible causes:**
- Service not running
- Request not reaching gateway
- Wrong version deployed

**Solution:**
1. Check service status: `sudo systemctl status flow-evm-gateway`
2. Check version: `sudo journalctl -u flow-evm-gateway -n 10 | grep version`
3. Check if request reaches gateway (check network/firewall)

### Issue: Only API logs, no validator logs
**Possible causes:**
- Validation is failing before reaching simulateValidation
- Log level too high (Debug logs filtered)

**Solution:**
1. Check for validation errors in API logs
2. Check raw logs: `sudo journalctl -u flow-evm-gateway -n 100 --no-pager`

### Issue: Logs show but no detailed info
**Possible causes:**
- Old version deployed (doesn't have enhanced logging)
- Log level filtering Debug logs

**Solution:**
1. Verify version: `sudo journalctl -u flow-evm-gateway -n 10 | grep version`
2. Check if Info-level logs show (they should always show)

## Expected Log Flow

When you submit a UserOp, you should see this sequence:

1. **API receives request**:
   ```
   "component":"userop-api" "message":"received eth_sendUserOperation request"
   ```

2. **Pre-validation logging**:
   ```
   "component":"userop-validator" "message":"calling EntryPoint.simulateValidation with full UserOp details"
   ```

3. **Either success or error**:
   - Success: UserOp added to pool
   - Error: Validation failed with detailed error

## Debugging Tips

1. **Always check raw logs first** - filters might hide important info
2. **Check version** - make sure enhanced logging version is deployed
3. **Check component names** - logs use "userop-api" and "userop-validator"
4. **Check log levels** - Info and Error should always show, Debug might be filtered

