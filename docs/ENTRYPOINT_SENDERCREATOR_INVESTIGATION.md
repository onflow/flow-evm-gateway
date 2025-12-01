# EntryPoint senderCreator Investigation

## Current Status

❌ **`senderCreator()` call is reverting** even with correct selector `0x4af63f02`

This suggests:
1. EntryPoint might not have a `senderCreator()` function
2. EntryPoint's senderCreator might be an immutable (no getter function)
3. Function signature might be different
4. EntryPoint version might not match expected v0.9.0

## Alternative Approaches

### Option 1: Check EntryPoint Storage

If `senderCreator` is stored in EntryPoint's storage, we can read it directly:

```bash
# Try different storage slots (immutables are typically in specific slots)
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_getStorageAt",
    "params":["0xcf1e8398747a05a997e8c964e957e47209bdff08", "0x0", "latest"]
  }'
```

### Option 2: Check EntryPoint Bytecode

Verify EntryPoint has the expected bytecode:

```bash
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_getCode",
    "params":["0xcf1e8398747a05a997e8c964e957e47209bdff08", "latest"]
  }'
```

Then search the bytecode for the selector `4af63f02` to see if the function exists.

### Option 3: Direct SenderCreator Address

According to deployment docs, SenderCreator should be at `0x1681B9f3a0F31F27B17eCb1b6cC1e3aC0C130dCb`. 

**Maybe EntryPoint doesn't need to call `senderCreator()` - maybe it's hardcoded or we should use the known address directly?**

### Option 4: Check if EntryPoint Uses Different Pattern

EntryPoint v0.9.0 might:
- Have senderCreator as an immutable (no getter)
- Use a different function name
- Require different calling pattern

## What the Contract Agent Said

The contract agent confirmed:
- ✅ Contracts are correctly deployed
- ✅ `senderCreator()` works correctly
- ✅ SenderCreator contract exists

But our test shows it's reverting. This suggests:
- **Gateway might be calling a different EntryPoint** (wrong address?)
- **EntryPoint might be on a different network/chain**
- **EntryPoint might need special initialization**

## Next Steps

1. **Verify EntryPoint address** - Confirm gateway is calling the correct EntryPoint
2. **Check EntryPoint bytecode** - Verify it matches expected v0.9.0
3. **Try using known SenderCreator address directly** - Maybe EntryPoint doesn't need to call the function
4. **Check if EntryPoint needs initialization** - Maybe senderCreator needs to be set after deployment

## Key Question

**Does EntryPoint v0.9.0 actually have a `senderCreator()` getter function, or is senderCreator an immutable that's set in the constructor?**

If it's an immutable, there might not be a getter function, and we should use the known address `0x1681B9f3a0F31F27B17eCb1b6cC1e3aC0C130dCb` directly.

