# Factory Address Mismatch Analysis

## Expected Factory Address (from deployment docs)
**SimpleAccountFactory**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`

## Actual Factory Address (from calldata initCode)
**From calldata**: `0x582e9f1433c8bc371c391b0f59c1e15da8affc9d1`

## Comparison

- **Expected**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` (20 bytes)
- **Actual**: `0x582e9f1433c8bc371c391b0f59c1e15da8affc9d1` (20 bytes)

**These do NOT match!**

The actual factory address starts with `58` but expected starts with `2e`.

## Impact

If the factory address is wrong, EntryPoint will try to call a non-existent or incorrect factory contract, which will cause:
1. Account creation to fail
2. EntryPoint to revert with empty reason (because the call fails)

## Next Steps

1. **Verify the factory address in the frontend/client code**
   - Check what factory address the client is using in the initCode
   - It should be `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`

2. **If the client is using the wrong factory address**, update it to use the correct one:
   - `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`

3. **If the client is using the correct factory address**, then there's a decoding issue in how we're extracting it from the calldata.

## How to Verify

The initCode in the calldata should be:
```
0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12  (factory address, 20 bytes)
+ 25fbfb9c  (function selector, 4 bytes - need to verify this)
+ 0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b17159  (owner, 32 bytes)
+ 0000000000000000000000000000000000000000000000000000000000000000  (salt, 32 bytes)
```

Total: 20 + 4 + 32 + 32 = 88 bytes ✅ (matches initCodeLen: 88)

