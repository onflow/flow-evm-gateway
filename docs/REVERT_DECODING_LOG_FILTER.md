# Revert Decoding Log Filter Commands

## Primary Command (Recommended)

Watch for UserOperation validation logs with revert decoding:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "userop|sendUserOperation|simulateValidation|entrypoint|validation|revert|decodedResult|isValidationResult|isFailedOp|aaErrorCode|EntryPoint version verified|senderCreator"
```

## Specific Filters

### 1. Watch for Validation Results (Success Cases)

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "isValidationResult.*true|simulateValidation succeeded"
```

**What to look for:**
- `"isValidationResult":true` → Validation passed
- `"message":"simulateValidation succeeded"` → Success

### 2. Watch for Validation Failures

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "isFailedOp.*true|aaErrorCode|simulateValidation failed"
```

**What to look for:**
- `"isFailedOp":true` → Validation failed
- `"aaErrorCode":"AA13"` → Specific error code
- `"message":"simulateValidation failed"` → Failure

### 3. Watch for EntryPoint Version Verification

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "EntryPoint version verified|senderCreator.*call failed"
```

**What to look for:**
- `"message":"EntryPoint version verified"` → Version check passed
- `"message":"senderCreator() call failed"` → Version check failed (might be wrong version/ABI)

### 4. Watch for All Revert Decoding Info

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "decodedResult|revertReasonHex|revertDataLen|errorSelector"
```

**What to look for:**
- `"decodedResult":"..."` → Decoded error message
- `"revertReasonHex":"0x..."` → Raw revert data
- `"revertDataLen":...` → Length of revert data
- `"errorSelector":"0x..."` → Error selector (for custom errors)

### 5. Comprehensive Filter (All UserOp Activity)

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "userop|sendUserOperation|simulateValidation|entrypoint|validation|revert|decodedResult|isValidationResult|isFailedOp|aaErrorCode|EntryPoint version|senderCreator|ownerFromInitCode|recoveredSigner|signerMatchesOwner"
```

## Key Log Fields to Watch

### Success Indicators
- `"isValidationResult":true` → Validation passed
- `"decodedResult":"ValidationResult(...)"` → Success with gas estimates

### Failure Indicators
- `"isFailedOp":true` → Validation failed
- `"aaErrorCode":"AA13"` → Specific error (AA13 = initCode failed)
- `"aaErrorCode":"AA20"` → Account not deployed
- `"aaErrorCode":"AA23"` → Account reverted

### Version Verification
- `"message":"EntryPoint version verified"` → v0.9.0 confirmed
- `"senderCreator":"0x1681B9f3a0F31F27B17eCb1b6cC1e3aC0C130dCb"` → SenderCreator address

### Decoding Status
- `"decodedResult":"..."` → Successfully decoded
- `"errorSelector":"0x..."` → Custom error detected
- `"revertDataLen":0` → Empty revert (might be issue)

## Example Log Output

### Success Case
```json
{
  "level":"info",
  "component":"userop-validator",
  "isValidationResult":true,
  "decodedResult":"ValidationResult(preOpGas=50000, paid=0, validAfter=0, validUntil=0)",
  "message":"simulateValidation succeeded - ValidationResult indicates validation passed"
}
```

### Failure Case
```json
{
  "level":"error",
  "component":"userop-validator",
  "isFailedOp":true,
  "aaErrorCode":"AA13",
  "decodedResult":"FailedOp(opIndex=0, reason=\"AA13 initCode failed or OOG\")",
  "message":"simulateValidation failed - validation error detected"
}
```

### Version Verification
```json
{
  "level":"info",
  "component":"userop-validator",
  "entryPoint":"0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "senderCreator":"0x1681B9f3a0F31F27B17eCb1b6cC1e3aC0C130dCb",
  "message":"EntryPoint version verified - senderCreator() exists (likely v0.9.0)"
}
```

