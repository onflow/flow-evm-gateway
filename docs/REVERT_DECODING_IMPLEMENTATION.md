# Revert Reason Decoding Implementation

## Summary

Enhanced the gateway's revert error handling to decode EntryPoint revert reasons using multiple strategies. This helps identify why EntryPoint validation is failing even when revert reasons appear empty.

## Implementation

### Multi-Strategy Decoding

The `decodeRevertReason()` function attempts to decode revert data using four strategies:

#### Strategy 1: Standard Error(string)

Decodes standard Solidity `Error(string)` reverts:
- Selector: `0x08c379a0`
- Format: `selector (4 bytes) + offset (32 bytes) + length (32 bytes) + string data`
- Example: `Error(string): insufficient balance`

#### Strategy 2: Custom Error Selectors

Identifies EntryPoint v0.9.0 custom errors:
- Detects non-standard error selectors
- Logs selector and data length for manual investigation
- Common EntryPoint errors:
  - `ValidationResult` (varies by EntryPoint version)
  - `FailedOp(uint256 opIndex, string reason)` (varies by EntryPoint version)
- Example: `Custom error (selector: 0x12345678, data length: 64 bytes) - may be EntryPoint ValidationResult or FailedOp`

**Note**: Full decoding of custom errors requires the complete EntryPoint ABI with error definitions. The gateway currently identifies them but cannot decode the parameters without the full ABI.

#### Strategy 3: Empty Revert Detection

Identifies simple reverts without reason:
- Detects reverts with ≤4 bytes (selector only or empty)
- Example: `Revert without reason (empty or selector only)`

#### Strategy 4: Raw String Detection

Attempts to decode as raw UTF-8 string:
- Removes null padding
- Checks if data is printable ASCII
- Useful for non-standard revert formats
- Example: `Raw string: validation failed`

## Usage

The decoder is automatically called when EntryPoint validation reverts:

```go
// In simulateValidation()
if revertErr, ok := err.(*errs.RevertError); ok {
    decodedReason := v.decodeRevertReason(revertData, revertErr.Reason)
    // Log decoded reason if available
    if decodedReason != "" {
        v.logger.Error().
            Str("decodedRevertReason", decodedReason).
            Msg("decoded EntryPoint revert reason")
    }
}
```

## Logging

When a revert is decoded, the gateway logs:

```json
{
  "level": "error",
  "decodedRevertReason": "Error(string): insufficient balance",
  "revertReasonHex": "0x08c379a0...",
  "message": "decoded EntryPoint revert reason"
}
```

For custom errors:

```json
{
  "level": "debug",
  "errorSelector": "0x12345678",
  "revertDataHex": "0x12345678...",
  "revertDataLen": 64,
  "message": "EntryPoint revert with custom error selector (not Error(string))"
}
```

## Limitations

1. **Custom Error Decoding**: Cannot fully decode EntryPoint custom errors without the complete ABI
   - Solution: Add full EntryPoint v0.9.0 ABI with error definitions
   - Workaround: Log selector and data length for manual investigation

2. **Debug Trace**: Cannot use `debug_traceCall` from validator
   - Reason: Validator doesn't have access to `DebugAPI`
   - Solution: Would require refactoring to pass `DebugAPI` to validator
   - Workaround: Enhanced logging provides sufficient context for most cases

## Future Improvements

1. **Add Full EntryPoint ABI**: Include all EntryPoint v0.9.0 error definitions
2. **Error Selector Database**: Map known error selectors to error names
3. **Debug Trace Integration**: Add optional debug trace when revert reason is empty
4. **SimpleAccount Error Decoding**: Decode SimpleAccount-specific errors

## Testing

To test revert decoding:

1. Submit a UserOp that will fail validation
2. Check logs for `decodedRevertReason` field
3. Verify the decoded reason matches the actual EntryPoint error

## Example Output

**Before** (empty revert):
```json
{
  "revertReasonHex": "0x",
  "revertDataLen": 0,
  "message": "EntryPoint.simulateValidation reverted"
}
```

**After** (with decoding):
```json
{
  "revertReasonHex": "0x08c379a00000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000e696e73756666696369656e742062616c616e6365000000000000000000000000",
  "revertDataLen": 100,
  "decodedRevertReason": "Error(string): insufficient balance",
  "message": "decoded EntryPoint revert reason"
}
```

