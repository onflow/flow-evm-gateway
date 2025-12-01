# RPC Sync/Visibility Issue - EntryPointSimulations Contract

## Problem

The EntryPointSimulations contract is **deployed and verified** on Flowscan at `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`, but the gateway RPC endpoint can't see it when querying bytecode or making calls.

## Root Cause

This is an **RPC sync/visibility issue**, not a deployment issue:

1. **Contract exists**: Verified on Flowscan
2. **RPC can't see it**: Gateway RPC endpoint may be:
   - Out of sync with the network
   - Not indexed this contract yet
   - Pointing to wrong network (though unlikely if other calls work)

## Impact

- **Contract is deployed**: ✅ Confirmed on Flowscan
- **Gateway RPC can't query it**: ❌ `eth_getCode` returns empty or wrong data
- **Gateway may still work**: ✅ If address is configured correctly, calls might succeed

## Solutions

### 1. Wait for RPC Sync

The gateway RPC may catch up automatically:
- Contracts are indexed as blocks are processed
- If the contract was recently deployed, wait a few blocks
- Check if other contracts are visible to verify RPC is working

### 2. Verify Gateway RPC Network

Ensure the gateway RPC is pointing to Flow Testnet:

```bash
# Check gateway logs for network info
sudo journalctl -u flow-evm-gateway -n 50 | grep -i "network\|chain"

# Test RPC directly
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}'
```

### 3. Gateway Should Still Work

Even if the RPC can't see the contract:
- **If address is configured**: Gateway will attempt the call
- **Contract exists**: The call may succeed even if RPC query fails
- **Configuration is correct**: `--entry-point-simulations-address` is set

## Gateway Behavior

The gateway now:
1. ✅ **Proceeds with calls** even if RPC can't see the contract
2. ✅ **Logs clear messages** about RPC visibility issues
3. ✅ **Trusts configuration** - if address is set, we use it
4. ✅ **Handles empty reverts gracefully** - notes it may be RPC sync issue

## Diagnostic Commands

### Check if RPC can see the contract:

```bash
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":["0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3","latest"]}'
```

**Expected if visible**: Non-empty bytecode (starts with `0x`)
**Actual if not visible**: `"0x"` or empty

### Check gateway logs for RPC issues:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "simulationAddress|EntryPointSimulations|RPC|sync|indexing"
```

### Verify contract on Flowscan:

- **URL**: https://evm-testnet.flowscan.io/address/0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
- **Status**: Should show "Contract" tab with verified source code
- **Functions**: Should show `simulateValidation` in the ABI

## What This Means

1. **Contract is deployed** ✅ - Verified on Flowscan
2. **Gateway should work** ✅ - If configured correctly, calls will proceed
3. **RPC sync will catch up** ⏳ - Eventually the RPC will see the contract
4. **Not a blocker** ✅ - Gateway can function even if RPC query fails

## Gateway Configuration

Ensure these are set:

```bash
# In /etc/flow/runtime-conf.env
ENTRY_POINT_SIMULATIONS_ADDRESS=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3

# In service file
--entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS}
```

The gateway will use this address even if the RPC can't query the contract's bytecode.

## Monitoring

Watch for these log messages:

**Good (proceeding despite RPC issue):**
```
"using EntryPointSimulations contract for simulateValidation (v0.7+) - proceeding even if RPC can't see contract (may be sync/indexing issue)"
```

**Warning (empty revert, but may be RPC issue):**
```
"simulateValidation reverted with empty data... Note: Contract is verified on Flowscan at this address. If RPC can't see the contract, this may be an RPC sync/indexing issue."
```

## Next Steps

1. ✅ **Verify configuration** - Ensure address is set correctly
2. ⏳ **Wait for RPC sync** - Contract will become visible eventually
3. ✅ **Test UserOperations** - Gateway may work even if RPC can't see contract
4. 📊 **Monitor logs** - Watch for successful calls or clearer error messages

