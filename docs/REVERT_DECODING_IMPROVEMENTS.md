# Revert Decoding Improvements

## Summary

Implemented comprehensive improvements to EntryPoint revert data decoding based on critical feedback about ERC-4337 EntryPoint v0.9 behavior.

## Key Changes

### 1. Updated Understanding of simulateValidation

**Before:**
- ❌ Treated revert as failure
- ❌ Looked for success return values
- ❌ "Empty revert" meant mystery error

**After:**
- ✅ Revert is expected behavior (EntryPoint design)
- ✅ Revert data contains the result (ValidationResult or FailedOp)
- ✅ Must decode revert payload to determine success/failure

### 2. Enhanced Revert Decoding

**New `decodeRevertData()` function:**
- Decodes `FailedOp(uint256,string)` errors
- Decodes `FailedOpWithRevert(uint256,string,bytes)` errors
- Attempts to decode `ValidationResult` struct (success case)
- Extracts AAxx error codes (AA10, AA13, AA20, AA23, etc.)
- Handles standard `Error(string)` reverts
- Logs unknown formats for investigation

**RevertDecodeResult struct:**
```go
type RevertDecodeResult struct {
    Decoded            string // Human-readable decoded message
    IsValidationResult bool   // True if this is a ValidationResult (success)
    IsFailedOp         bool   // True if this is a FailedOp error
    AAErrorCode        string // AAxx error code if detected
}
```

### 3. Updated Gateway Logic

**Before:**
- All reverts treated as failures
- Returned error on any revert

**After:**
- `ValidationResult` → Success (validation passed)
- `FailedOp` → Failure (validation failed)
- AAxx errors → Failure with specific error code
- Unknown format → Warning, treat as failure for safety

### 4. EntryPoint Version Verification

**New `VerifyEntryPointVersion()` function:**
- Calls `senderCreator()` to verify EntryPoint is v0.9.0
- Logs warning if verification fails (doesn't block validation)
- Helps identify ABI/version mismatches

### 5. Updated EntryPoint ABI

**Added:**
- `senderCreator()` function definition
- `FailedOp` error definition
- `FailedOpWithRevert` error definition
- `EncodeSenderCreator()` helper function

## Error Code Detection

The gateway now extracts AAxx error codes from revert messages:
- **AA10**: Account already exists
- **AA13**: initCode failed or OOG
- **AA20**: Account not deployed
- **AA21**: Didn't pay prefund
- **AA22**: Expired or not due
- **AA23**: Account reverted (validateUserOp failed)

## Example Decoded Errors

**FailedOp:**
```
FailedOp(opIndex=0, reason="AA13 initCode failed or OOG")
```

**ValidationResult (Success):**
```
ValidationResult(preOpGas=50000, paid=0, validAfter=0, validUntil=0)
```

**AA Error Code:**
```
validation failed: FailedOp(opIndex=0, reason="AA20 account not deployed") (AA error: AA20)
```

## Next Steps

1. **Test with actual UserOperations** to see decoded errors
2. **Verify EntryPoint codehash** matches official v0.9
3. **Improve ValidationResult decoding** if format differs
4. **Add more AAxx error code handling** as needed

## Files Modified

- `services/requester/userop_validator.go`:
  - Added `decodeRevertData()` function
  - Added `RevertDecodeResult` struct
  - Updated `simulateValidation()` to handle ValidationResult as success
  - Added `VerifyEntryPointVersion()` function
  - Added AAxx error code extraction

- `services/requester/entrypoint_abi.go`:
  - Added `senderCreator()` function
  - Added error definitions (`FailedOp`, `FailedOpWithRevert`)
  - Added `EncodeSenderCreator()` helper

## Testing

After deployment, check logs for:
- `isValidationResult: true` → Validation passed
- `isFailedOp: true` → Validation failed
- `aaErrorCode: "AA13"` → Specific error code
- `decodedResult: "..."` → Human-readable error message

