# Zero Hash Issue - UserOperation Returns Zero Hash Instead of Error

## Problem

When submitting a UserOperation via `eth_sendUserOperation`, the gateway returns a zero hash (`0x0000000000000000000000000000000000000000000000000000000000000000`) instead of a valid UserOp hash or an error message.

## Root Cause Analysis

The issue had two parts:

1. **"Entity not found" error during validation**: The validator was using `GetLatestEVMHeight()` which returns the network's latest height (e.g., 80755566). However, `Call()` reads from the local database. If the gateway hasn't indexed that block yet, it returns "entity not found" because the block doesn't exist in the local database.

2. **Zero hash returned instead of error**: When validation fails, `SendUserOperation` returns `(common.Hash{}, err)`. The go-ethereum RPC framework should convert this to a JSON-RPC error response, but the zero hash was being returned as the result instead.

## Changes Made

1. **Fixed Block Height Issue**: Changed the validator to use `blocks.LatestEVMHeight()` (latest indexed height) instead of `requester.GetLatestEVMHeight()` (network's latest height). This ensures validation only queries blocks that exist in the local database.

2. **Added Safety Check**: Added explicit validation to ensure we never return a zero hash as a valid result (even though this should never happen).

3. **Enhanced Error Logging**: Added detailed logging when validation fails, including:

   - When requests are received
   - EntryPoint address and block height used
   - Detailed validation errors with context

4. **Added Blocks Storage to Validator**: Updated `UserOpValidator` to have access to blocks storage so it can query the latest indexed height.

## Debugging Steps

### 1. Check Gateway Logs

SSH to your EC2 instance and check the gateway logs:

```bash
# Check recent logs
sudo journalctl -u flow-evm-gateway -n 100 --no-pager | grep -i "user operation\|validation\|error"

# Follow logs in real-time
sudo journalctl -u flow-evm-gateway -f
```

Look for:

- `"user operation validation failed"` - This will show the actual validation error
- `"simulation failed"` - EntryPoint validation failure
- `"signature verification failed"` - Signature validation failure
- Any other error messages related to UserOperations

### 2. Test with curl

Test the UserOperation submission directly:

```bash
curl -X POST http://localhost:8545 \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "eth_sendUserOperation",
    "params": [
      {
        "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
        "nonce": "0x0",
        "initCode": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d125fbfb9cf0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b171590000000000000000000000000000000000000000000000000000000000000000",
        "callData": "0x",
        "callGasLimit": "0x186a0",
        "verificationGasLimit": "0x186a0",
        "preVerificationGas": "0x5208",
        "maxFeePerGas": "0x1",
        "maxPriorityFeePerGas": "0x1",
        "paymasterAndData": "0x",
        "signature": "0x73ac7ad341e40d5f68df0e79eb1925a6d081cce4514520212473083abdbcf21a3a9769ae8752231864cfe7a31fb24c34f8b87dbfb082c4cfa539c56caed98a7601"
      },
      "0xcf1e8398747a05a997e8c964e957e47209bdff08"
    ]
  }'
```

### 3. Common Validation Errors

Based on the UserOperation provided, common validation failures include:

1. **Signature Validation**:

   - For account creation, signature should be validated against the owner address from `initCode`, not the sender
   - Signature format: `v` should be `0x00` or `0x01` (not `27` or `28`) for SimpleAccount
   - Signature is over the UserOp hash: `keccak256(keccak256(packedUserOp) || entryPoint || chainId)`

2. **EntryPoint Simulation**:

   - `EntryPoint.simulateValidation` must succeed
   - This validates the signature against the owner for account creation
   - This validates the account state and nonce

3. **Gas Parameters**:

   - `maxFeePerGas` and `maxPriorityFeePerGas` must be positive (>= 1)
   - Gas limits must be reasonable (<= 10M)

4. **Nonce**:
   - Nonce must match the expected nonce from EntryPoint
   - For account creation, nonce should be `0`

## Expected Behavior

According to ERC-4337 specification:

**Success Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x<64-char-hex-hash>"
}
```

**Error Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32000,
    "message": "UserOperation validation failed: <detailed reason>"
  }
}
```

## Fix Applied

The code now includes:

1. **Block Height Fix**: Validator uses indexed height instead of network height (fixes "entity not found")
2. **Safety Check**: Prevents zero hash from being returned as valid result
3. **Enhanced Logging**: Detailed error logging with EntryPoint address, block height, and validation context
4. **Proper Error Propagation**: Errors are properly logged and should propagate through `handleError`

## initCode Length Clarification

**Note**: The gateway logs show `initCodeLen:88` while the client reports `178 bytes`. This is **not a bug**:

- **Client's "178 bytes"** = Hex string length (including `0x` prefix)
- **Gateway's "88 bytes"** = Actual decoded byte length
- **Calculation**: (178 hex chars - 2 for `0x`) / 2 = 88 bytes

The gateway is correctly reporting the byte length. There is no truncation or parsing issue.

## Next Steps

1. **Check Gateway Logs**: The logs should now show detailed validation errors
2. **Verify Error Response**: The RPC framework should return proper JSON-RPC error responses
3. **If Zero Hash Still Returned**: This indicates a bug in the RPC framework or middleware that needs investigation

## Related Files

- `api/userop_api.go` - RPC handler for `eth_sendUserOperation`
- `services/requester/userop_validator.go` - UserOperation validation logic (fixed to use indexed height)
- `bootstrap/bootstrap.go` - Updated to pass blocks storage to validator
- `api/utils.go` - Error handling utilities

## Technical Details

### The "Entity Not Found" Fix

**Before:**

```go
// Used network's latest height (may not be indexed yet)
height, err := v.requester.GetLatestEVMHeight(ctx)
```

**After:**

```go
// Uses latest indexed height (guaranteed to exist in database)
height, err := v.blocks.LatestEVMHeight()
```

This ensures that when `Call()` queries the EntryPoint contract, it uses a block height that exists in the local database, preventing "entity not found" errors.
