# EntryPoint Contract Deployment Guide

## Standard EntryPoint Contract

**Yes, there is a standard EntryPoint contract** defined by the ERC-4337 specification. The official implementation is maintained by **eth-infinitism** and is the reference implementation used across the Ethereum ecosystem.

## Official Repository

**GitHub**: https://github.com/eth-infinitism/account-abstraction

This repository contains:
- EntryPoint contract implementations (v0.6, v0.7, v0.9+)
- Reference bundler implementation
- Testing utilities
- Deployment scripts

## EntryPoint Versions

### EntryPoint v0.6 (Recommended for Initial Deployment)

- **Status**: Stable, widely deployed
- **Canonical Address** (if using CREATE2): `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789`
- **Features**: Core ERC-4337 functionality
- **Compatibility**: Works with OpenZeppelin PaymasterERC20

### EntryPoint v0.7

- **Status**: Newer version with improvements
- **Features**: Enhanced security and gas optimization
- **Compatibility**: Some newer paymasters (e.g., Coinbase VerifyingPaymaster)

### EntryPoint v0.9+

- **Status**: Latest version
- **Features**: Paymaster signature standardization (`PAYMASTER_SIG_MAGIC`)
- **Compatibility**: Most modern implementations

**Recommendation**: Start with **EntryPoint v0.6** for maximum compatibility with existing infrastructure.

## Deterministic Deployment

The EntryPoint contract uses **CREATE2** for deterministic deployment, ensuring the same address across all networks when deployed with the same salt and bytecode.

### Benefits

1. **Consistency**: Same address on all networks
2. **Compatibility**: Works with existing bundlers, wallets, and tools
3. **Trust**: Well-known, audited address
4. **Infrastructure**: Existing tooling expects this address

### Deployment Address

When deployed using the standard CREATE2 salt from the eth-infinitism repository:
- **v0.6**: `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789`

**Note**: This address is deterministic only if you use the exact same deployment script and salt from the official repository.

## Deployment Steps

### Prerequisites

1. **Node.js** and **npm/yarn** installed
2. **Hardhat** or **Foundry** for deployment
3. **Deployer Account** with sufficient FLOW for:
   - Contract deployment gas
   - Initial EntryPoint deposit (if needed)

### Step 1: Clone Official Repository

```bash
git clone https://github.com/eth-infinitism/account-abstraction.git
cd account-abstraction
```

### Step 2: Install Dependencies

```bash
yarn install
# or
npm install
```

### Step 3: Configure Network

Edit `hardhat.config.ts` to add Flow EVM network:

```javascript
import { HardhatUserConfig } from "hardhat/config";

const config: HardhatUserConfig = {
  networks: {
    "flow-evm": {
      url: "https://evm-gateway.flow.com", // Gateway RPC endpoint
      chainId: 747, // Flow EVM Chain ID (update with actual)
      accounts: process.env.DEPLOYER_PRIVATE_KEY 
        ? [process.env.DEPLOYER_PRIVATE_KEY]
        : [],
    },
  },
  // ... other config
};

export default config;
```

### Step 4: Set Up Deployer Account

Create a `.env` file or set environment variables:

```bash
# Option 1: Private key
export DEPLOYER_PRIVATE_KEY=0x...

# Option 2: Mnemonic (for Hardhat)
export MNEMONIC_FILE=./mnemonic.txt
```

### Step 5: Deploy EntryPoint

#### Using Hardhat (from eth-infinitism repo)

```bash
# Deploy EntryPoint v0.6
yarn hardhat deploy --network flow-evm

# Or use the specific deployment script
yarn hardhat run scripts/deploy-entrypoint.js --network flow-evm
```

#### Using Foundry

```bash
# If the repo has Foundry support
forge script script/DeployEntryPoint.s.sol --rpc-url $FLOW_EVM_RPC --broadcast
```

### Step 6: Verify Deployment

1. **Check Address**: Verify the deployed address matches expected (if using CREATE2)
2. **Verify Contract**: Verify contract on block explorer (if available)
3. **Test Functions**: Call `getNonce()` or `getDeposit()` to verify functionality

### Step 7: Update Gateway Configuration

Update `config/config.go` or runtime configuration:

**For Flow Testnet (v0.9.0)**:
```go
EntryPointAddress: common.HexToAddress("0xcf1e8398747a05a997e8c964e957e47209bdff08"), // v0.9.0
```

**For Canonical v0.6.0**:
```go
EntryPointAddress: common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"), // v0.6
```

**For Custom Deployment**:
```go
EntryPointAddress: common.HexToAddress("<DEPLOYED_ADDRESS>"),
```

**Note**: See `docs/FLOW_TESTNET_DEPLOYMENT.md` for complete Flow Testnet deployment configuration.

## Alternative: Use Existing Deployment

If EntryPoint is already deployed on Flow EVM by another party:

1. **Verify Address**: Confirm the deployed address
2. **Verify Source**: Ensure it's the official EntryPoint contract
3. **Check Version**: Verify it's v0.6, v0.9, or compatible version
4. **Update Config**: Set `EntryPointAddress` in gateway configuration

**Flow Testnet**: EntryPoint v0.9.0 is already deployed at `0xcf1e8398747a05a997e8c964e957e47209bdff08`. See `docs/FLOW_TESTNET_DEPLOYMENT.md` for details.

## EntryPoint Contract Interface

The EntryPoint contract provides these key functions:

### For Bundlers

- `handleOps(UserOperation[] ops, address payable beneficiary)`: Execute UserOperations
- `getDeposit(address account)`: Get deposit balance for account/paymaster

### For Validation

- `simulateValidation(UserOperation calldata userOp)`: Simulate validation (reverts on failure)

### For Paymasters

- `depositTo(address account)`: Deposit native currency for paymaster
- `withdrawTo(address payable withdrawAddress, uint256 withdrawAmount)`: Withdraw deposit

## Security Considerations

1. **Non-Upgradable**: EntryPoint is non-upgradable by design (security feature)
2. **Audited**: The official implementation is extensively audited
3. **Deterministic**: CREATE2 deployment ensures trust through determinism
4. **Immutable**: Once deployed, contract cannot be changed

## Testing

After deployment, test EntryPoint functionality:

```javascript
// Test getDeposit
const entryPoint = await ethers.getContractAt("IEntryPoint", ENTRY_POINT_ADDRESS);
const deposit = await entryPoint.getDeposit(paymasterAddress);
console.log("Paymaster deposit:", deposit.toString());

// Test simulateValidation (should revert for invalid UserOp)
try {
  await entryPoint.simulateValidation(userOp);
  console.log("Validation passed");
} catch (error) {
  console.log("Validation failed (expected for test):", error.message);
}
```

## Network-Specific Considerations

### Flow EVM

- **Chain ID**: Update with actual Flow EVM Chain ID
- **Native Currency**: FLOW (18 decimals)
- **Gas Price**: May be fixed or use EIP-1559
- **Block Time**: Flow-specific block times

### Testnet vs Mainnet

- Deploy to testnet first for testing
- Use same deployment process for mainnet
- Consider using different addresses for testnet/mainnet (or same if using CREATE2)

## References

- **Official Repository**: https://github.com/eth-infinitism/account-abstraction
- **ERC-4337 Specification**: https://eips.ethereum.org/EIPS/eip-4337
- **EntryPoint Documentation**: https://docs.erc4337.io/smart-accounts/entrypoint-explainer
- **EntryPoint Contract**: https://github.com/eth-infinitism/account-abstraction/blob/develop/contracts/core/EntryPoint.sol

## Summary

✅ **Use the official EntryPoint contract** from eth-infinitism
✅ **Deploy EntryPoint v0.6** for maximum compatibility
✅ **Use CREATE2** for deterministic address (if possible)
✅ **Verify deployment** before updating gateway config
✅ **Test thoroughly** on testnet before mainnet deployment

The EntryPoint contract is the **standard, audited, production-ready** implementation used across the Ethereum ecosystem. No need to write a custom implementation.

