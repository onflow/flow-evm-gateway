# Checking UserOp Validation Logs

## Current Status

Your gateway is running `testnet-v1-fix-handleops-encoding` ✅

EntryPoint version verification is working ✅

## To See UserOp Validation Logs

### Option 1: Broader Filter (Recommended)

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -E "userop-validator|SendUserOperation|simulateValidation|validation|revert|EntryPoint"
```

This will show:
- UserOp API activity (`SendUserOperation`)
- Validation activity (`userop-validator`)
- EntryPoint simulation calls
- Revert reasons

### Option 2: Watch for Specific UserOp Hash

If you just submitted a UserOp with hash `0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a`:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|SendUserOperation|simulateValidation"
```

### Option 3: All UserOp-Related Logs

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|sendUserOperation|validation|simulation|entrypoint"
```

## What to Look For

### When UserOp is Submitted

You should see:
```
"component":"userop-api"
"endpoint":"SendUserOperation"
"message":"received eth_sendUserOperation request"
```

### During Validation

You should see:
```
"component":"userop-validator"
"message":"calling EntryPoint.simulateValidation with full UserOp details"
```

### If Validation Succeeds

You should see:
```
"component":"userop-validator"
"message":"EntryPoint.simulateValidation succeeded"
```

OR if using EntryPointSimulations:
```
"simulationAddress":"0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3"
"message":"calling EntryPointSimulations.simulateValidation"
```

### If Validation Fails

You should see:
```
"component":"userop-validator"
"error":"..."
"revertReasonHex":"..."
"message":"EntryPoint.simulateValidation reverted"
```

## Check Bundler Activity

Since the fix is deployed, also check if bundler is processing UserOps:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "bundler|pendingUserOpCount|created handleOps|submitted bundled"
```

## Current Filter Issue

Your current filter is looking for very specific fields that might not all be present:
- `decodedResult` - Only in certain error cases
- `isValidationResult` - Only if validation succeeds with structured result
- `isFailedOp` - Only if FailedOp error is decoded
- `aaErrorCode` - Only if AAxx error code is present
- `revertReasonHex` - Only if revert has data
- `revertDataLen` - Always present, but might be 0

**Try the broader filters above** to see all validation activity.

