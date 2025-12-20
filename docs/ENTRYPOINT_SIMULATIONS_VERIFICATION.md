# EntryPointSimulations Contract Verification Issue

## Problem

The gateway is now correctly calling EntryPointSimulations at `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`, but the contract doesn't have the `simulateValidation` function with selector `0xee219423`.

**Error:**
```
simulateValidation not implemented on this contract (selector 0xee219423 not found in bytecode at 0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3)
```

## Root Cause

The EntryPointSimulations contract deployed at that address either:
1. **Doesn't have `simulateValidation` function** - Wrong contract deployed
2. **Has different function signature** - Function exists but with different selector
3. **Is not EntryPointSimulations** - Wrong address or contract type

## Verification Steps

### 1. Check Contract Bytecode

```bash
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_getCode",
    "params":["0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3", "latest"]
  }'
```

**Expected:** Non-empty bytecode (contract exists)

### 2. Check if Selector Exists in Bytecode

The selector `0xee219423` should be present in the bytecode if `simulateValidation` exists.

You can search the bytecode response for `ee219423` to see if it's there.

### 3. Verify Contract Address

Confirm with the contract deployment team that:
- `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3` is the correct EntryPointSimulations address
- The contract was deployed correctly
- The contract has the `simulateValidation` function

### 4. Check Function Signature

The gateway expects:
```
simulateValidation(UserOperation)
```

Where `UserOperation` is:
```
(address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)
```

**Selector:** `0xee219423` (calculated from full signature)

If the deployed contract uses a different signature, the selector will be different.

## What the Gateway Now Does

The gateway will:
1. **Check if selector exists** in EntryPointSimulations bytecode before calling
2. **Fail fast** with clear error if function doesn't exist
3. **Log detailed information** about the contract and selector

## Next Steps

1. **Verify EntryPointSimulations contract:**
   - Check if contract at `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3` is correct
   - Verify it has `simulateValidation` function
   - Check function signature matches expected format

2. **If contract is wrong:**
   - Redeploy EntryPointSimulations with correct implementation
   - Update gateway config with correct address

3. **If function signature differs:**
   - Get the actual function signature from deployed contract
   - Update gateway ABI to match

4. **Temporary workaround (NOT RECOMMENDED):**
   - Could skip simulation validation (risky)
   - Or use a different simulation method

## Diagnostic Command

After rebuild, check logs for:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -E "simulateValidation.*not found|functionSelector|simulationCodeLen|verified simulateValidation"
```

You should see:
- `"simulateValidation function selector not found in EntryPointSimulations bytecode"` - Function doesn't exist
- `"verified simulateValidation function exists"` - Function exists (good!)

## Expected Behavior

**If function exists:**
- Gateway verifies selector in bytecode
- Calls simulateValidation
- Gets proper revert data (ValidationResult or FailedOp)

**If function doesn't exist:**
- Gateway detects selector missing in bytecode
- Fails with clear error message
- Logs contract address and selector for debugging

