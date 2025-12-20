# Enhanced Logging for EntryPoint Validation Debugging

## Summary

Added comprehensive logging enhancements to help debug EntryPoint validation failures. The gateway now logs detailed information about UserOperations, signature recovery, owner extraction, and EntryPoint revert reasons.

## Changes Made

### 1. Owner Address Extraction from initCode

**Function**: `extractOwnerFromInitCode(initCode []byte)`

- Extracts the owner address from `SimpleAccountFactory.createAccount(owner, salt)` initCode
- Format: `factoryAddress (20 bytes) + functionSelector (4 bytes) + encoded params`
- Owner address is at offset 36 (after factory + selector + offset padding)

**Usage**: Automatically extracts owner when `initCode` is present (account creation)

### 2. Signature Recovery Logging

**Enhancement**: Recover signer address from signature before calling EntryPoint

- Uses EIP-191 signing: `keccak256("\x19\x01" || chainId || userOpHash)`
- Recovers the signer address from the signature
- Compares recovered signer with owner from initCode (for account creation)

### 3. Enhanced Pre-Validation Logging

**Before calling `simulateValidation`, the gateway now logs**:

- All UserOp fields (sender, nonce, gas parameters, etc.)
- UserOp hash (calculated by gateway)
- Chain ID
- Owner address (extracted from initCode if present)
- Recovered signer address (from signature)
- Whether signer matches owner (for account creation)
- Signature v value (0 or 1 for SimpleAccount)

**Example log entry**:
```json
{
  "level": "debug",
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
  "userOpHash": "0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82",
  "nonce": "0",
  "initCodeLen": 88,
  "callDataLen": 0,
  "maxFeePerGas": "1000000000",
  "maxPriorityFeePerGas": "1000000000",
  "callGasLimit": "100000",
  "verificationGasLimit": "100000",
  "preVerificationGas": "21000",
  "signatureLen": 65,
  "height": 80624561,
  "chainID": "545",
  "ownerFromInitCode": "0x3cC530e139Dd93641c3F30217B20163EF8b17159",
  "ownerExtracted": true,
  "recoveredSigner": "0x3cC530e139Dd93641c3F30217B20163EF8b17159",
  "signatureRecovered": true,
  "signerMatchesOwner": true,
  "signatureV": 0,
  "message": "calling EntryPoint.simulateValidation"
}
```

### 4. Enhanced Revert Error Logging

**When EntryPoint validation reverts, the gateway now logs**:

- All the same context as pre-validation logs
- Revert reason hex string
- Revert data length
- Owner address (if extracted)
- Recovered signer address
- Whether signer matches owner

**Example revert log**:
```json
{
  "level": "error",
  "revertReasonHex": "0x",
  "revertDataLen": 0,
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
  "userOpHash": "0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82",
  "nonce": "0",
  "initCodeLen": 88,
  "height": 80624561,
  "ownerFromInitCode": "0x3cC530e139Dd93641c3F30217B20163EF8b17159",
  "recoveredSigner": "0x3cC530e139Dd93641c3F30217B20163EF8b17159",
  "signerMatchesOwner": true,
  "message": "EntryPoint.simulateValidation reverted"
}
```

## What This Helps Debug

### 1. Signature Validation Issues

- **Recovered signer vs owner**: If `signerMatchesOwner` is `false`, the signature was signed by the wrong address
- **Signature v value**: Should be 0 or 1 for SimpleAccount (not 27/28)

### 2. UserOp Structure Issues

- **All gas parameters**: Can verify they match client expectations
- **initCode length**: Should be 88 bytes for SimpleAccountFactory.createAccount
- **UserOp hash**: Gateway's calculated hash (should match client)

### 3. EntryPoint Validation Issues

- **Revert reason**: Even if empty, we log all context to help identify the issue
- **Block height**: Using indexed height (not network's latest)
- **Chain ID**: Verifies correct chain ID is used in hash calculation

## Next Steps

1. **Deploy the enhanced logging** to the gateway
2. **Test with a UserOp** and capture the logs
3. **Compare logs with client-side calculations**:
   - UserOp hash should match
   - Recovered signer should match owner
   - All gas parameters should match
4. **If EntryPoint still reverts**, the logs will show:
   - Whether signature recovery works correctly
   - Whether owner extraction works correctly
   - Whether signer matches owner
   - All UserOp parameters being sent to EntryPoint

## Revert Reason Decoding

The gateway now attempts to decode revert reasons using multiple strategies:

1. **Standard Error(string)**: Decodes `Error(string)` reverts (selector `0x08c379a0`)
2. **Custom Error Selectors**: Identifies EntryPoint v0.9.0 custom errors (ValidationResult, FailedOp) by selector
3. **Raw String Detection**: Attempts to decode as raw UTF-8 string if no standard format matches
4. **Empty Revert Detection**: Identifies simple reverts without reason

**Example decoded errors**:
- `Error(string): insufficient balance`
- `Custom error (selector: 0x12345678, data length: 64 bytes) - may be EntryPoint ValidationResult or FailedOp`
- `Revert without reason (empty or selector only)`

## Future Enhancements

If EntryPoint continues to revert with empty reason, we can:

1. **Add debug_traceCall support**: Use the gateway's `debug_traceCall` API to trace EntryPoint execution
   - **Note**: This would require passing `DebugAPI` to the validator, which is a larger refactor
   - For now, the enhanced logging should provide enough context to diagnose issues
2. **Full EntryPoint ABI with errors**: Add complete EntryPoint v0.9.0 ABI including all custom error definitions
3. **Add SimpleAccount validation logging**: If we can call SimpleAccount directly, log its validation results

## Files Modified

- `services/requester/userop_validator.go`:
  - Added `extractOwnerFromInitCode()` function
  - Enhanced `simulateValidation()` with detailed logging
  - Added signature recovery before EntryPoint call
  - Enhanced revert error logging with all context

