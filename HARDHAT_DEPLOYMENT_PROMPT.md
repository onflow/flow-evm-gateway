# Hardhat Deployment Prompt for ERC-4337 Contracts

Use this prompt in your Hardhat project to deploy the necessary ERC-4337 contracts for testing with the Flow EVM Gateway.

---

## Prompt to Use

```
I'm working on a project that integrates with the Flow EVM Gateway's ERC-4337 (Account Abstraction) implementation. I need to deploy the following contracts to Flow testnet for testing:

1. **EntryPoint Contract (v0.6)**: The standard eth-infinitism EntryPoint contract
   - Repository: https://github.com/eth-infinitism/account-abstraction
   - Version: v0.6.0 (recommended for stability)
   - This is the canonical EntryPoint contract that processes UserOperations
   - The gateway expects this at a specific address (configurable via EntryPointAddress)

2. **OpenZeppelin PaymasterERC20 Contract**: A paymaster that allows users to pay gas fees with ERC-20 tokens
   - This should be compatible with the EntryPoint v0.6
   - Needs to support the standard paymaster interface

3. **SimpleAccount and SimpleAccountFactory**: Required for testing UserOperation creation
   - **SimpleAccount**: Basic smart account implementation from eth-infinitism that can execute UserOperations
   - **SimpleAccountFactory**: Factory contract to deploy new SimpleAccount instances
   - Repository: https://github.com/eth-infinitism/account-abstraction (same as EntryPoint)
   - Version: v0.6.0 (must match EntryPoint version)
   - These are required for end-to-end testing of the ERC-4337 flow

**Requirements:**
- Deploy to Flow testnet (or local Flow emulator if testing locally)
- Use Hardhat with Flow network configuration
- The EntryPoint should be deployed using CREATE2 with a fixed salt (0x0000000000000000000000000000000000000000000000000000000000000000) to ensure deterministic address
- Set up proper initialization and configuration
- Include deployment scripts that can be run via `npx hardhat run scripts/deploy.js --network flow-testnet`
- Export deployment addresses to a JSON file for easy configuration

**Configuration Context:**
- The gateway's EntryPoint address is configured via `EntryPointAddress` in config
- Default EntryPoint address used in tests: `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789` (standard address on most networks)
- The gateway supports bundling UserOperations and wrapping them in Cadence transactions
- Paymaster validation checks for sufficient deposit via `EntryPoint.getDeposit()`

**Additional Notes:**
- Reference the official EntryPoint deployment guide: https://github.com/eth-infinitism/account-abstraction/blob/v0.6.0/specs/EntryPoint.md
- The EntryPoint contract is a singleton - only one should exist per network
- PaymasterERC20 needs to be initialized with a token address and configured with the EntryPoint
- Consider deploying a test ERC-20 token for PaymasterERC20 to use

Please help me:
1. Set up the Hardhat project structure with proper dependencies
2. Create deployment scripts for EntryPoint, PaymasterERC20, SimpleAccountFactory, and SimpleAccount implementation
3. Configure the network settings for Flow testnet
4. Create a deployment verification script
5. Export deployment addresses in a format that can be easily imported into the gateway configuration
6. Include a script to deploy a test SimpleAccount instance using the factory
```

---

## Additional Context for the AI

If you need to provide more context, you can add:

```
**Project Structure:**
- I have a Hardhat project initialized in [project-path]
- I need contracts deployed to Flow testnet
- The gateway is configured to use EntryPoint at address: [your-entrypoint-address]
- I want to test the full ERC-4337 flow: UserOperation submission → bundling → execution

**Specific Contract Requirements:**
- EntryPoint: Must be the exact eth-infinitism v0.6.0 implementation
- PaymasterERC20: Should match OpenZeppelin's implementation compatible with EntryPoint v0.6
- SimpleAccount: Use the exact SimpleAccount.sol from eth-infinitism v0.6.0
- SimpleAccountFactory: Use the exact SimpleAccountFactory.sol from eth-infinitism v0.6.0
- All contracts must be from the same version (v0.6.0) for compatibility

**Testing Goals:**
- Deploy contracts and verify they work with the gateway
- Test UserOperation submission via `eth_sendUserOperation`
- Test paymaster sponsorship
- Verify event indexing works correctly
```

---

## Expected Output

The AI should help you create:

1. **Hardhat Configuration** (`hardhat.config.js`)
   - Flow testnet network configuration
   - Proper compiler settings for Solidity

2. **Deployment Scripts** (`scripts/deploy.js` or similar)
   - EntryPoint deployment with CREATE2
   - PaymasterERC20 deployment and initialization
   - SimpleAccountFactory deployment
   - SimpleAccount implementation deployment (or reference to existing bytecode)
   - Script to deploy a test SimpleAccount instance

3. **Contract Files** (if not using npm packages)
   - EntryPoint.sol (from eth-infinitism v0.6.0)
   - PaymasterERC20.sol (OpenZeppelin compatible)
   - SimpleAccount.sol (from eth-infinitism v0.6.0)
   - SimpleAccountFactory.sol (from eth-infinitism v0.6.0)

4. **Deployment Artifacts**
   - JSON file with deployed addresses
   - Verification scripts

5. **Configuration Helper**
   - Script to update gateway config with deployed addresses

---

## Quick Reference: Gateway Configuration

After deployment, update your gateway configuration:

```go
// In config/config.go or your config file
EntryPointAddress: common.HexToAddress("0x..."), // Your deployed EntryPoint
BundlerEnabled: true,
MaxOpsPerBundle: 10,
UserOpTTL: 5 * time.Minute,
BundlerBeneficiary: common.HexToAddress("0x..."), // Your bundler address
```

---

## Useful Links

- EntryPoint Repository: https://github.com/eth-infinitism/account-abstraction
- EntryPoint v0.6.0 Release: https://github.com/eth-infinitism/account-abstraction/releases/tag/v0.6.0
- OpenZeppelin Contracts: https://github.com/OpenZeppelin/openzeppelin-contracts
- Flow EVM Gateway Documentation: See `docs/ENTRYPOINT_DEPLOYMENT.md` and `docs/OPENZEPPELIN_PAYMASTER.md`

