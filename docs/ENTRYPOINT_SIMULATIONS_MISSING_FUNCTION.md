# EntryPointSimulations Function Missing - Root Cause Identified

## Problem

The gateway is correctly calling EntryPointSimulations at `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`, but the contract **does not have** the `simulateValidation` function with selector `0xee219423`.

**Error:**

```
simulateValidation not implemented on this contract (selector 0xee219423 not found in bytecode at 0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3)
```

## Verification

**Bytecode Check:**

- Contract bytecode length: 23,681 bytes (contract exists)
- Selector `0xee219423`: **NOT FOUND** ❌
- Selector `0x7213331f` (alternative): **NOT FOUND** ❌

**Contract Explorer:**

- [FlowScan Contract Page](https://evm-testnet.flowscan.io/address/0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3?tab=contract)

## Root Cause

The contract at `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3` either:

1. **Is not EntryPointSimulations** - Wrong contract type
2. **Has different function signature** - Function exists but with different selector
3. **Was not deployed correctly** - Missing function in deployment
4. **Is a different version** - Different EntryPointSimulations implementation

## Expected Function Signature

The gateway expects:

```solidity
function simulateValidation(UserOperation calldata userOp) external;
```

Where `UserOperation` is:

```solidity
struct UserOperation {
    address sender;
    uint256 nonce;
    bytes initCode;
    bytes callData;
    uint256 callGasLimit;
    uint256 verificationGasLimit;
    uint256 preVerificationGas;
    uint256 maxFeePerGas;
    uint256 maxPriorityFeePerGas;
    bytes paymasterAndData;
    bytes signature;
}
```

**Expected Selector:** `0xee219423` (calculated from full signature)

## What the Gateway Does Now

The gateway now:

1. ✅ **Checks if selector exists** in EntryPointSimulations bytecode before calling
2. ✅ **Fails fast** with clear error if function doesn't exist
3. ✅ **Logs detailed information** about contract address, selector, and bytecode length

## Next Steps

### 1. Verify Contract Address

Confirm with the deployment team:

- Is `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3` the correct EntryPointSimulations address?
- Was the contract deployed correctly?
- Does it have the `simulateValidation` function?

### 2. Check Contract Source Code

If the contract is verified on FlowScan:

- View the contract source code
- Check if `simulateValidation` exists
- Verify the function signature matches expected format

### 3. Get Correct Address

If the address is wrong:

- Get the correct EntryPointSimulations address
- Update gateway configuration with correct address

### 4. Redeploy Contract (if needed)

If the contract is missing the function:

- Redeploy EntryPointSimulations with correct implementation
- Ensure `simulateValidation` function is included
- Update gateway configuration with new address

## Diagnostic Commands

### Check Contract Bytecode

```bash
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":["0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3","latest"]}'
```

### Check Gateway Logs

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "simulateValidation.*not found|functionSelector|simulationCodeLen|verified simulateValidation"
```

## Current Status

- ✅ Gateway correctly detects missing function
- ✅ Gateway fails with clear error message
- ❌ EntryPointSimulations contract missing required function
- ⏳ Waiting for correct contract address or redeployment

## Related Files

- `services/requester/userop_validator.go` - Validation logic with function existence check
- `services/requester/entrypoint_abi.go` - ABI definition for simulateValidation
- `config/config.go` - EntryPointSimulationsAddress configuration
