# Diagnostic Log Command - EntryPoint Validation

## Primary Diagnostic Command

This command shows exactly what we need to diagnose EntryPoint validation:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -E "decodedResult|isValidationResult|isFailedOp|aaErrorCode|revertReasonHex|revertDataLen|errorSelector|EntryPoint version verified|senderCreator.*call failed|simulateValidation (succeeded|failed|reverted)|EntryPoint\.simulateValidation|returnedDataHex|returnedDataLen|errorCode|errorMessage|RPC call returned error"
```

## What This Shows

### Critical Fields for Diagnosis:

1. **`decodedResult`** - The decoded error message (FailedOp, ValidationResult, etc.)
2. **`isValidationResult`** - If true, validation PASSED (this is success!)
3. **`isFailedOp`** - If true, validation FAILED
4. **`aaErrorCode`** - Specific AAxx error code (AA13, AA20, AA23, etc.)
5. **`revertReasonHex`** - Raw revert data (hex encoded)
6. **`revertDataLen`** - Length of revert data
7. **`errorSelector`** - Error selector (for custom errors)
8. **`EntryPoint version verified`** - Confirms v0.9.0
9. **`senderCreator.*call failed`** - Indicates version/ABI mismatch

### Success Indicators:
- `"isValidationResult":true` → Validation passed!
- `"decodedResult":"ValidationResult(...)"` → Success with gas estimates
- `"message":"simulateValidation succeeded"` → Success

### Failure Indicators:
- `"isFailedOp":true` → Validation failed
- `"aaErrorCode":"AA13"` → initCode failed or OOG
- `"aaErrorCode":"AA20"` → Account not deployed
- `"aaErrorCode":"AA23"` → Account reverted
- `"message":"simulateValidation failed"` → Failure

## Alternative: Even More Specific

If you want to see ONLY the revert decoding results:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "decodedResult|isValidationResult|isFailedOp|aaErrorCode|simulateValidation (succeeded|failed)"
```

## What We're Looking For

When you send a UserOperation, you should see:

1. **EntryPoint version check:**
   ```
   "message":"EntryPoint version verified - senderCreator() exists (likely v0.9.0)"
   ```
   OR
   ```
   "message":"senderCreator() call failed - EntryPoint might not be v0.9.0 or ABI mismatch"
   ```

2. **simulateValidation call:**
   ```
   "message":"calling EntryPoint.simulateValidation with full UserOp details"
   ```

3. **Revert with decoding:**
   ```
   "decodedResult":"..."
   "isValidationResult":true OR "isFailedOp":true
   "aaErrorCode":"AA13" (if failure)
   ```

4. **Final result:**
   ```
   "message":"simulateValidation succeeded - ValidationResult indicates validation passed"
   ```
   OR
   ```
   "message":"simulateValidation failed - validation error detected"
   ```

## If You See Nothing

1. **Check if service is running:**
   ```bash
   sudo systemctl status flow-evm-gateway
   ```

2. **Check if new code is deployed:**
   ```bash
   sudo journalctl -u flow-evm-gateway -n 10 --no-pager | grep "version"
   ```

3. **Check if UserOps are being sent:**
   ```bash
   sudo journalctl -u flow-evm-gateway -f | grep "SendUserOperation"
   ```

4. **See ALL logs (no filter):**
   ```bash
   sudo journalctl -u flow-evm-gateway -f
   ```

