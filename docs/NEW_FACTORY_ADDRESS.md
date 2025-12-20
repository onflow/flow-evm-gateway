# New SimpleAccountFactory Address

## Updated Factory Address

**New Address**: `0x472153734AEfB3FD24b8129b87F146A5939aC2AF`  
**Date**: December 8, 2025  
**Network**: Flow Testnet (Chain ID: 545)

## What Changed

The new factory address deploys SimpleAccount **directly without proxy**, fixing the `msg.sender` issue that caused AA23 errors.

### Previous Factory (Deprecated)
- **Address**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- **Issue**: Used ERC1967Proxy pattern, causing `msg.sender` to be proxy address instead of EntryPoint
- **Result**: SimpleAccount's authorization checks failed → AA23 errors

### New Factory (Current)
- **Address**: `0x472153734AEfB3FD24b8129b87F146A5939aC2AF`
- **Fix**: Deploys SimpleAccount directly (no proxy)
- **Result**: `msg.sender` == EntryPoint (correct) → Authorization checks pass ✅

## Gateway Status

✅ **Gateway requires NO changes** - it automatically extracts the factory address from UserOp's `initCode`.

The gateway is account-agnostic and works with any factory address. Simply update your frontend/client to use the new factory address in the `initCode`.

## Frontend/Client Update

Update your frontend code to use the new factory address:

```javascript
// Old factory (deprecated)
// const SIMPLE_ACCOUNT_FACTORY = "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12";

// New factory (current)
const SIMPLE_ACCOUNT_FACTORY = "0x472153734AEfB3FD24b8129b87F146A5939aC2AF";
```

## Verification

After updating to the new factory:
- ✅ Account creation should succeed
- ✅ `validateUserOp()` should pass (msg.sender == EntryPoint)
- ✅ `execute()` should succeed
- ✅ No AA23 errors related to proxy `msg.sender` issues

## Explorer Links

- **New Factory**: https://evm-testnet.flowscan.io/address/0x472153734AEfB3FD24b8129b87F146A5939aC2AF
- **EntryPoint**: https://evm-testnet.flowscan.io/address/0xcf1e8398747a05a997e8c964e957e47209bdff08

## Related Documentation

- `docs/PRODUCTION_ACCOUNT_COMPATIBILITY.md` - Gateway compatibility with production accounts
- `docs/REDEPLOY_SIMPLEACCOUNT_FIX.md` - Details on the proxy issue and fix
- `docs/FLOW_TESTNET_DEPLOYMENT.md` - Complete deployment configuration

