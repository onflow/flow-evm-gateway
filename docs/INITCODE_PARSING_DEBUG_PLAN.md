# InitCode Parsing Debug Plan

## Problem Statement

Client sends correct initCode (88 bytes), but gateway appears to see truncated factory address (19 bytes instead of 20).

## Current Status

✅ **Client Side:**
- initCode: 88 bytes exactly
- Factory address: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` (20 bytes)
- Function selector: `0x5fbfb9cf` (4 bytes)
- Hash matches gateway: `0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82`
- Signature recovery works: `signerMatchesOwner: true`

❌ **Gateway Side:**
- Reports seeing truncated factory address (19 bytes)
- EntryPoint reverts with empty reason

## Debugging Strategy

### Step 1: Verify Raw InitCode from RPC

**Location:** `api/userop_api.go` - Right after receiving request

**What to check:**
```go
if userOpArgs.InitCode != nil {
    logFields = logFields.
        Int("initCodeLen", len(*userOpArgs.InitCode)).  // Should be 88
        Str("initCodeHex", hexutil.Encode(*userOpArgs.InitCode))  // Full hex
    if len(*userOpArgs.InitCode) >= 24 {
        factoryAddr := common.BytesToAddress((*userOpArgs.InitCode)[0:20])
        selector := hexutil.Encode((*userOpArgs.InitCode)[20:24])
        logFields = logFields.
            Str("rawFactoryAddress", factoryAddr.Hex()).  // Should be 0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12
            Str("rawFunctionSelector", selector)  // Should be 0x5fbfb9cf
    }
}
```

**Expected log:**
```json
{
  "initCodeLen": 88,
  "initCodeHex": "0x2e9f1433c8bc371c391b0f59c1e15da8affc9d125fbfb9cf0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b171590000000000000000000000000000000000000000000000000000000000000000",
  "rawFactoryAddress": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12",
  "rawFunctionSelector": "0x5fbfb9cf"
}
```

**If this is wrong:** Issue is in JSON unmarshaling (`hexutil.Bytes`)

**If this is correct:** Issue is later in the pipeline

### Step 2: Verify Processed InitCode

**Location:** `services/requester/userop_validator.go` - After ToUserOperation()

**What to check:**
```go
if len(userOp.InitCode) >= 24 {
    factoryAddr := common.BytesToAddress(userOp.InitCode[0:20])
    selector := hexutil.Encode(userOp.InitCode[20:24])
    v.logger.Info().
        Str("factoryAddress", factoryAddr.Hex()).
        Str("functionSelector", selector).
        Int("initCodeLen", len(userOp.InitCode)).
        Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
        Msg("decoded initCode details")
}
```

**Expected log:**
```json
{
  "factoryAddress": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12",
  "functionSelector": "0x5fbfb9cf",
  "initCodeLen": 88,
  "initCodeHex": "0x2e9f1433c8bc371c391b0f59c1e15da8affc9d125fbfb9cf..."
}
```

**Compare to Step 1:**
- If `rawFactoryAddress` != `factoryAddress`: Issue is in `ToUserOperation()`
- If they match: Issue is in ABI encoding

### Step 3: Verify Calldata Encoding

**Location:** `services/requester/userop_validator.go` - Before EntryPoint call

**What to check:**
```go
logFields = logFields.Str("calldataHex", hexutil.Encode(calldata)).Int("calldataLen", len(calldata))
```

**Expected:** Calldata should contain the full 88-byte initCode embedded correctly.

**How to verify:**
1. Extract initCode from calldata (it's ABI-encoded as bytes field)
2. Compare to processed initCode from Step 2
3. If different: Issue is in ABI encoding

### Step 4: Manual Calldata Decoding

**Process:**
1. Get `calldataHex` from logs
2. Find initCode offset (should be `0x160` = 352 bytes)
3. Extract initCode length (should be `0x58` = 88 bytes)
4. Extract initCode data (should be 88 bytes)
5. Compare first 20 bytes to expected factory address

**Expected:**
```
Calldata at offset 352: 0x0000000000000000000000000000000000000000000000000000000000000058 (88 bytes)
Calldata at offset 384: 0x2e9f1433c8bc371c391b0f59c1e15da8affc9d12... (88 bytes of initCode)
First 20 bytes: 0x2e9f1433c8bc371c391b0f59c1e15da8affc9d12
```

## Potential Issues and Fixes

### Issue 1: JSON Unmarshaling Truncation

**Symptom:** `rawFactoryAddress` is wrong in Step 1

**Possible causes:**
- `hexutil.Bytes` has a bug
- JSON parser is truncating
- Client is actually sending truncated data (but logs show it's correct)

**Fix:**
- Check if `hexutil.Bytes` implementation has issues
- Add manual hex decoding as fallback
- Verify client is actually sending full data (check network request)

### Issue 2: ToUserOperation() Conversion Issue

**Symptom:** `rawFactoryAddress` is correct, but `factoryAddress` is wrong

**Possible causes:**
- Slice assignment issue: `uo.InitCode = *args.InitCode`
- Memory corruption
- Byte order issue

**Fix:**
- Add explicit copy: `uo.InitCode = make([]byte, len(*args.InitCode)); copy(uo.InitCode, *args.InitCode)`
- Verify no other code is modifying `userOp.InitCode`

### Issue 3: ABI Encoding Issue

**Symptom:** Both raw and processed are correct, but calldata is wrong

**Possible causes:**
- ABI encoder truncating bytes field
- Offset calculation wrong
- Length encoding wrong

**Fix:**
- Check `entryPointABIParsed.Pack()` implementation
- Manually verify ABI encoding
- Use alternative encoding method if needed

### Issue 4: EntryPoint Decoding Issue

**Symptom:** Calldata is correct, but EntryPoint sees wrong data

**Possible causes:**
- EntryPoint's ABI decoder has issue
- Network transmission issue
- EntryPoint contract bug

**Fix:**
- Use `debug_traceCall` to see what EntryPoint actually receives
- Verify EntryPoint contract code
- Check if there's a known EntryPoint bug

## Action Items

1. **Deploy new version** with raw initCode logging (`testnet-v1-raw-initcode-logging`)
2. **Send UserOp** and capture logs
3. **Compare values** at each step:
   - Raw initCode (Step 1)
   - Processed initCode (Step 2)
   - Calldata initCode (Step 3)
4. **Identify where truncation occurs**
5. **Fix the issue** at the identified location
6. **Verify fix** by checking all three values match

## Success Criteria

All three values should match:
- `rawFactoryAddress` = `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- `factoryAddress` = `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- Factory address in calldata = `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`

Once all three match, EntryPoint should be able to:
1. Extract correct factory address
2. Call factory correctly
3. Create account successfully
4. Validate signature successfully

