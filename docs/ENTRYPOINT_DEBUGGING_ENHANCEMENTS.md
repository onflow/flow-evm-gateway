# EntryPoint Validation Debugging Enhancements

## Summary

Enhanced the gateway's EntryPoint validation logging and error handling to provide detailed debugging information when `simulateValidation` fails with empty revert reasons.

## What Was Added

### 1. Enhanced Signature Logging

**Before calling EntryPoint.simulateValidation**, the gateway now logs:
- Full signature hex (`signatureHex`)
- Individual signature components (`signatureR`, `signatureS`, `signatureV`)
- Exact calldata being sent to EntryPoint (`calldataHex`, `calldataLen`)

**Example log entry**:
```json
{
  "level": "info",
  "component": "userop-validator",
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
  "userOpHash": "0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82",
  "signatureHex": "0x1d0eeb364b7997bcad9dd2e97ec381316e1baebfc918713140c74db244e848a114e9d057ebbf1bef53b9c33e2c334f0942a750a2b41fe857e4a484718c8380b81b",
  "signatureR": "0x1d0eeb364b7997bcad9dd2e97ec381316e1baebfc918713140c74db244e848a1",
  "signatureS": "0x14e9d057ebbf1bef53b9c33e2c334f0942a750a2b41fe857e4a484718c8380b8",
  "signatureV": 27,
  "calldataHex": "0x...",
  "calldataLen": 708,
  "message": "calling EntryPoint.simulateValidation with full UserOp details"
}
```

### 2. Enhanced Revert Reason Decoding

**When EntryPoint reverts**, the gateway now:
- **Decodes `FailedOp(uint256,string)` custom errors** - extracts opIndex and reason string
- **Decodes `FailedOpWithRevert(uint256,string,bytes)` custom errors** - extracts opIndex, reason, and revert data length
- Attempts to decode standard `Error(string)` reverts
- Logs other custom error selectors at **Info level** (not Debug) so they're always visible
- Provides context about what the empty revert might indicate

**Example decoded `FailedOp` error**:
```json
{
  "level": "error",
  "decodedRevertReason": "FailedOp(opIndex=0, reason=\"AA23 reverted\")",
  "revertReasonHex": "0x220266b6...",
  "message": "decoded EntryPoint revert reason"
}
```

**Example custom error log**:
```json
{
  "level": "info",
  "errorSelector": "0x12345678",
  "revertDataHex": "0x12345678...",
  "revertDataLen": 64,
  "message": "EntryPoint revert with custom error selector (not Error(string)) - may be EntryPoint ValidationResult or FailedOp"
}
```

**Example empty revert warning**:
```json
{
  "level": "warn",
  "revertReasonHex": "0x",
  "revertDataLen": 0,
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
  "message": "EntryPoint reverted with empty reason - this usually indicates SimpleAccount validation failed or EntryPoint internal validation failed. Consider using debug_traceCall to trace execution."
}
```

### 3. Calldata Logging

The gateway now logs the exact calldata being sent to EntryPoint:
- `calldataHex`: Full hex-encoded calldata
- `calldataLen`: Length of calldata in bytes

This allows you to:
- Verify the exact UserOp structure being sent
- Compare with what the client expects
- Debug encoding issues

## What's Available But Not Integrated

### debug_traceCall Support

The gateway **does support** `debug_traceCall` via the `DebugAPI`, but it's not directly integrated into the validator because:

1. **Architectural constraint**: The `UserOpValidator` doesn't have access to `DebugAPI` (which requires many dependencies like `registerStore`, `receipts`, `transactions`, etc.)
2. **Refactoring required**: Adding `debug_traceCall` to the validator would require significant refactoring to pass `DebugAPI` through the dependency chain

**Workaround**: You can manually call `debug_traceCall` on the gateway's RPC endpoint to trace EntryPoint execution:

```bash
curl -X POST http://<gateway-ip>:8545 \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "debug_traceCall",
    "params": [{
      "to": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
      "data": "0x<calldata-from-logs>"
    }, "latest", {
      "tracer": "callTracer"
    }]
  }'
```

**Future enhancement**: We can add optional `debug_traceCall` integration if needed, but it would require refactoring the validator to accept a `DebugAPI` interface.

## Current Status

### ✅ Working
- Signature recovery (recovered signer matches owner)
- Owner extraction from initCode
- UserOp hash calculation (matches client)
- Enhanced logging for all UserOp parameters
- Calldata logging
- Custom error selector detection

### ❌ Still Failing
- EntryPoint.simulateValidation reverting with empty reason
- SimpleAccount validation (likely, but need confirmation)

## Next Steps

1. **Test with enhanced logging**: The new logs will show:
   - Exact signature format being sent to EntryPoint
   - Exact calldata being sent
   - Any custom error selectors if present

2. **Compare signature format**: Check if the signature `v` value matches what SimpleAccount expects:
   - Gateway logs show `signatureV: 27` (from recovery)
   - But EntryPoint might expect `v=0` or `v=1` for SimpleAccount
   - The signature is passed directly to EntryPoint, so if the client sends `v=27`, EntryPoint receives `v=27`

3. **Use debug_traceCall manually**: If needed, use the gateway's `debug_traceCall` endpoint to trace EntryPoint execution and see exactly where it reverts

4. **Check SimpleAccount contract**: Verify:
   - SimpleAccount is deployed correctly
   - SimpleAccount's `_validateSignature` function exists
   - SimpleAccount expects the signature format being sent

## Signature Format Note

**Important**: The gateway logs show `signatureV: 27` because that's what the client sends. The gateway does **not** convert `v=27` to `v=0` before sending to EntryPoint - it passes the signature exactly as received from the client.

If SimpleAccount expects `v=0` or `v=1` (recovery ID format), but the client is sending `v=27` (EIP-155 format), EntryPoint/SimpleAccount will reject it.

**Question for frontend**: Are you sending `v=27` or `v=0/1`? SimpleAccount typically expects `v=0` or `v=1` (recovery ID), not `v=27/28` (EIP-155 format).

## Example: Using debug_traceCall

If you want to trace EntryPoint execution manually:

1. **Get calldata from logs**: Copy `calldataHex` from the gateway logs
2. **Call debug_traceCall**:
   ```bash
   curl -X POST http://3.150.43.95:8545 \
     -H 'Content-Type: application/json' \
     --data '{
       "jsonrpc": "2.0",
       "id": 1,
       "method": "debug_traceCall",
       "params": [{
         "to": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
         "data": "<calldataHex-from-logs>"
       }, "latest", {
         "tracer": "callTracer"
       }]
     }'
   ```
3. **Analyze trace**: The trace will show:
   - Which functions EntryPoint calls
   - Where exactly the revert happens
   - What the stack looks like at revert point

## Summary

The gateway now provides comprehensive logging for EntryPoint validation debugging:
- ✅ Full signature details (r, s, v, hex)
- ✅ Exact calldata being sent
- ✅ Enhanced revert reason decoding
- ✅ Custom error selector detection
- ✅ Context about empty reverts

The signature recovery is working correctly, so the issue is likely:
1. Signature format mismatch (client sending `v=27` but SimpleAccount expects `v=0/1`)
2. SimpleAccount validation failing for another reason
3. EntryPoint internal validation failing

The enhanced logs will help identify which of these is the cause.

