# Root Cause Analysis - EntryPoint Validation Failure

## ✅ What's Working

1. **Hash Calculation**: ✅ Matches between client and gateway
2. **Signature Recovery**: ✅ Successfully recovers signer
3. **Signer Matches Owner**: ✅ `0x3cC530e139Dd93641c3F30217B20163EF8b17159`
4. **Signature Format**: ✅ Correct (v=0, recovery ID format)

## ❌ Root Cause

**Wrong Factory Address in Client's initCode**

The client is sending an incorrect factory address in the UserOperation's `initCode`:

- **Client is sending**: `0x582e9f1433c8bc371c391b0f59c1e15da8affc9d` (19 bytes, missing last byte)
- **Should be**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` (20 bytes)

## Impact

When EntryPoint tries to create the account during `simulateValidation`:
1. It calls the factory at the wrong address
2. The call fails (factory doesn't exist or is wrong contract)
3. EntryPoint reverts with empty reason (because the call failed)

## Solution

**Update the frontend/client code to use the correct factory address:**

```javascript
// Correct SimpleAccountFactory address on Flow Testnet
const SIMPLE_ACCOUNT_FACTORY = "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12";
```

## Verification

After updating the factory address, the initCode should be:
```
0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12  (factory address, 20 bytes)
+ 25fbfb9c  (createAccount selector, 4 bytes)
+ 0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b17159  (owner, 32 bytes)
+ 0000000000000000000000000000000000000000000000000000000000000000  (salt, 32 bytes)
```

Total: 88 bytes ✅

## Additional Notes

- The gateway is correctly passing through the client's initCode
- The gateway's signature validation is working correctly
- The issue is purely in the client's factory address configuration
- Once the client uses the correct factory address, EntryPoint should be able to create the account and validate the signature successfully

## Deployment Reference

See `docs/FLOW_TESTNET_DEPLOYMENT.md` for all contract addresses:
- **EntryPoint**: `0xcf1e8398747a05a997e8c964e957e47209bdff08`
- **SimpleAccountFactory**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` ✅

