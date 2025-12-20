# Gateway InitCode Parsing Debug

## Problem

The client is sending correct initCode data:
- Factory address: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` (20 bytes) ✅
- Function selector: `0x5fbfb9cf` (correct) ✅
- initCode length: 88 bytes ✅

But the gateway appears to be seeing:
- Truncated factory address (19 bytes instead of 20)
- Shifted function selector

This suggests a gateway-side parsing issue.

## What We've Added

### 1. Raw InitCode Logging (NEW)

Added logging in `api/userop_api.go` to capture the **exact initCode as received** from the RPC request, before any processing:

```go
if userOpArgs.InitCode != nil {
    logFields = logFields.
        Int("initCodeLen", len(*userOpArgs.InitCode)).
        Str("initCodeHex", hexutil.Encode(*userOpArgs.InitCode))
    // Log factory address if initCode is long enough
    if len(*userOpArgs.InitCode) >= 24 {
        factoryAddr := common.BytesToAddress((*userOpArgs.InitCode)[0:20])
        selector := hexutil.Encode((*userOpArgs.InitCode)[20:24])
        logFields = logFields.
            Str("rawFactoryAddress", factoryAddr.Hex()).
            Str("rawFunctionSelector", selector)
    }
}
```

This will show:
- `initCodeHex`: The exact hex string as received
- `rawFactoryAddress`: Factory address extracted from bytes 0-19
- `rawFunctionSelector`: Function selector from bytes 20-23

### 2. Processed InitCode Logging (EXISTING)

The validator already logs the initCode after conversion to `UserOperation`:
- `factoryAddress`: Factory address from processed initCode
- `functionSelector`: Function selector from processed initCode
- `initCodeHex`: Full initCode hex after processing

## What to Look For

After redeploying with version `testnet-v1-raw-initcode-logging`, check the logs for:

1. **Raw initCode (from RPC request)**:
   ```
   "initCodeHex": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d125fbfb9cf..."
   "rawFactoryAddress": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12"
   "rawFunctionSelector": "0x5fbfb9cf"
   ```

2. **Processed initCode (after ToUserOperation)**:
   ```
   "factoryAddress": "0x..."
   "functionSelector": "0x..."
   "initCodeHex": "0x..."
   ```

## Comparison

Compare the raw vs processed values:

- **If raw is correct but processed is wrong**: Issue is in `ToUserOperation()` conversion
- **If raw is already wrong**: Issue is in JSON unmarshaling (`hexutil.Bytes`)
- **If both are correct but calldata is wrong**: Issue is in ABI encoding

## Expected Values

From client logs:
- Factory: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- Selector: `0x5fbfb9cf` (note: this is 4 bytes, not the standard `0x25fbfb9c` - need to verify which is correct)
- initCode length: 88 bytes

## Next Steps

1. Rebuild and redeploy with version `testnet-v1-raw-initcode-logging`
2. Send a UserOp and check logs for both raw and processed initCode
3. Compare values to identify where the corruption occurs
4. Fix the parsing issue at the identified location

## Log Filter Command

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "rawFactoryAddress|rawFunctionSelector|initCodeHex|factoryAddress|functionSelector|decoded initCode"
```

