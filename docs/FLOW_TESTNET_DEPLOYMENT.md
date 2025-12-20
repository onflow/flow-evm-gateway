# Flow Testnet ERC-4337 Deployment Configuration

This document contains the deployed contract addresses for ERC-4337 (Account Abstraction) on Flow Testnet.

**Deployment Date**: November 22, 2025  
**Network**: Flow Testnet (Chain ID: 545)  
**RPC Endpoint**: https://testnet.evm.nodes.onflow.org  
**Block Explorer**: https://evm-testnet.flowscan.io

## Contract Addresses

### EntryPoint (v0.9.0)
**Address**: `0xcf1e8398747a05a997e8c964e957e47209bdff08`

- **Version**: v0.9.0 (from eth-infinitism/account-abstraction)
- **Deployment Method**: CREATE2 with custom salt
- **Purpose**: Core EntryPoint contract that processes UserOperations
- **Status**: ✅ Deployed and verified
- **Explorer**: https://evm-testnet.flowscan.io/address/0xcf1e8398747a05a997e8c964e957e47209bdff08

**Note**: The canonical EntryPoint address `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789` contains v0.6.0 which is incompatible with SimpleAccountFactory (requires `senderCreator()` method). A new v0.9.0 EntryPoint was deployed for compatibility.

**SenderCreator Address**: `0x1681B9f3a0F31F27B17eCb1b6cC1e3aC0C130dCb`

### SimpleAccountFactory
**Address**: `0x472153734AEfB3FD24b8129b87F146A5939aC2AF` (Updated December 8, 2025)

- **Contract**: SimpleAccountFactory.sol (v0.9.0)
- **Purpose**: Factory contract to create new SimpleAccount instances
- **Deployment Pattern**: Deploys SimpleAccount directly (no proxy) - fixes `msg.sender` issues
- **Constructor Parameters**:
  - EntryPoint: `0xcf1e8398747a05a997e8c964e957e47209bdff08`
- **Status**: ✅ Deployed and verified
- **Explorer**: https://evm-testnet.flowscan.io/address/0x472153734AEfB3FD24b8129b87F146A5939aC2AF

**Previous Factory Address** (deprecated): `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- This factory used ERC1967Proxy pattern which caused `msg.sender` issues
- Use the new factory address above for all new account creation

**Usage**: 
- To create a new SimpleAccount, use UserOperation with initCode containing:
  - Factory address: `0x472153734AEfB3FD24b8129b87F146A5939aC2AF`
  - Function call: `createAccount(address owner, uint256 salt)`

### PaymasterERC20
**Address**: `0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a`

- **Contract**: PaymasterERC20.sol
- **Purpose**: Paymaster that allows users to pay gas fees with ERC-20 tokens
- **Constructor Parameters**:
  - EntryPoint: `0xcf1e8398747a05a997e8c964e957e47209bdff08`
  - Token: `0x99C7A1c5eCf02d3Dd01D2B7F5936D6611E8473CD` (TestToken)
  - Token Price: 1.0 tokens per gas unit (1e18)
  - Owner: `0x3cC530e139Dd93641c3F30217B20163EF8b17159` (deployer)
- **Status**: ✅ Deployed and verified
- **Explorer**: https://evm-testnet.flowscan.io/address/0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a

### TestToken (ERC-20)
**Address**: `0x99C7A1c5eCf02d3Dd01D2B7F5936D6611E8473CD`

- **Contract**: TestTokenForPaymaster.sol
- **Name**: TestToken
- **Symbol**: TEST
- **Decimals**: 18
- **Initial Supply**: 1,000,000 TEST (1,000,000 * 10^18 wei)
- **Purpose**: Test ERC-20 token for PaymasterERC20 testing
- **Status**: ✅ Deployed and verified
- **Explorer**: https://evm-testnet.flowscan.io/address/0x99C7A1c5eCf02d3Dd01D2B7F5936D6611E8473CD

## Gateway Configuration

Update your Flow EVM Gateway configuration with the following:

```go
// In config/config.go or your configuration file
EntryPointAddress: common.HexToAddress("0xcf1e8398747a05a997e8c964e957e47209bdff08"),
BundlerEnabled: true,
MaxOpsPerBundle: 10,
UserOpTTL: 5 * time.Minute,
BundlerBeneficiary: common.HexToAddress("0x..."), // Address to receive bundler fees
```

Or via command-line flags:

```bash
./flow-evm-gateway run \
  --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
  --bundler-enabled=true \
  --max-ops-per-bundle=10 \
  --user-op-ttl=5m \
  --bundler-beneficiary=0x... \
  # ... other flags
```

## Version Compatibility

### EntryPoint v0.9.0 Compatibility

The gateway code is compatible with EntryPoint v0.9.0. The core methods used by the gateway have the same ABI in both v0.6 and v0.9:

- ✅ `handleOps(UserOperation[] ops, address payable beneficiary)` - Same ABI
- ✅ `simulateValidation(UserOperation calldata userOp)` - Same ABI
- ✅ `getDeposit(address account)` - Same ABI
- ✅ `UserOperationEvent` - Same event signature
- ✅ `UserOperationRevertReason` - Same event signature

**Note**: EntryPoint v0.9.0 includes additional features (like `senderCreator()`) that are required by SimpleAccountFactory v0.9.0, but these are not used by the gateway's bundler implementation.

## Next Steps

### 1. Fund PaymasterERC20

**Deposit Native Tokens (FLOW) to EntryPoint**:

```solidity
// Call PaymasterERC20.deposit() with FLOW tokens
// This deposits to EntryPoint for gas payments
paymasterERC20.deposit{value: amount}()
```

**Transfer ERC-20 Tokens to PaymasterERC20**:

```solidity
// Transfer TEST tokens to PaymasterERC20 for user payments
testToken.transfer(paymasterERC20Address, amount)
```

### 2. Test UserOperations

**Create a SimpleAccount**:
- Use SimpleAccountFactory at `0x472153734AEfB3FD24b8129b87F146A5939aC2AF`
- Call `createAccount(owner, salt)` via UserOperation initCode

**Send UserOperation with PaymasterERC20**:
- User must approve PaymasterERC20 to spend TEST tokens
- Include PaymasterERC20 address in UserOperation.paymasterAndData

### 3. Integration Testing

Test the following flows:
1. Create SimpleAccount via UserOperation
2. Send UserOperation with native token payment
3. Send UserOperation with PaymasterERC20 (ERC-20 token payment)
4. Verify transactions on Flow testnet explorer

## Important Notes

1. **EntryPoint Version**: The deployed EntryPoint is v0.9.0, not the canonical v0.6.0. This is required for compatibility with SimpleAccountFactory v0.9.0.

2. **CREATE2 Safety**: The EntryPoint was deployed using CREATE2 with a custom salt. This ensures a unique address and prevents conflicts.

3. **Test Account**: The predicted SimpleAccount address (`0x6505D8f5eEe226364FC1B76E1Ce5D3Cad3B89662`) must be created via UserOperation with initCode. Direct factory calls will fail because `createAccount` can only be called by EntryPoint's senderCreator.

4. **Gas Sponsorship**: Flow testnet currently sponsors all transactions (gas price = 0), but you may still need FLOW tokens in your account.

5. **Token Price**: PaymasterERC20 is configured with a token price of 1.0 tokens per gas unit (1e18). Adjust if needed for testing.

## Deployer Information

**Deployer Address**: `0x3cC530e139Dd93641c3F30217B20163EF8b17159`  
**Deployer Balance**: ~99,999.99 FLOW (at time of deployment)

## Support & Documentation

- **ERC-4337 Specification**: https://eips.ethereum.org/EIPS/eip-4337
- **eth-infinitism Repository**: https://github.com/eth-infinitism/account-abstraction
- **EntryPoint v0.9.0 Release**: https://github.com/eth-infinitism/account-abstraction/releases/tag/v0.9.0

