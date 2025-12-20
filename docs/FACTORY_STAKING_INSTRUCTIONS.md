# Factory Staking Instructions for EntryPoint v0.9.0

## Overview

According to ERC-4337 specification, **factory contracts must stake** with the EntryPoint to participate in account creation. This is a security requirement to prevent denial-of-service attacks.

## Current Contract Addresses (Flow Testnet)

- **EntryPoint**: `0x33860348ce61ea6cec276b1cf93c5465d1a92131`
- **SimpleAccountFactory**: `0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f`

## Minimum Stake Requirements

- **Testnet**: 1,000 FLOW (minimum)
- **Production**: 3,300 FLOW (minimum)
- **Unstake Delay**: 7 days (604,800 seconds) - minimum required

## How to Stake the Factory

### Method 1: Using ethers.js (Recommended)

```typescript
import { ethers } from "ethers";

const ENTRY_POINT_ADDRESS = "0x33860348ce61ea6cec276b1cf93c5465d1a92131";
const FACTORY_ADDRESS = "0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f";
const MINIMUM_STAKE = ethers.parseEther("1000"); // 1,000 FLOW for testnet
const UNSTAKE_DELAY_SEC = 604800; // 7 days in seconds

// Connect to Flow testnet RPC
const provider = new ethers.JsonRpcProvider(
  "https://testnet.evm.nodes.onflow.org"
);
const signer = new ethers.Wallet(YOUR_PRIVATE_KEY, provider);

// Get EntryPoint contract
const entryPointABI = [
  "function addStake(uint32 unstakeDelaySec) payable",
  "function getDepositInfo(address account) view returns (tuple(uint256 deposit, bool staked, uint256 stake, uint32 unstakeDelaySec, uint48 withdrawTime))",
];
const entryPoint = new ethers.Contract(
  ENTRY_POINT_ADDRESS,
  entryPointABI,
  signer
);

// Check current stake status
const depositInfo = await entryPoint.getDepositInfo(FACTORY_ADDRESS);
console.log("Current stake:", depositInfo.stake.toString());
console.log("Is staked:", depositInfo.staked);
console.log("Unstake delay:", depositInfo.unstakeDelaySec.toString());

// If not staked or stake is insufficient, add stake
if (!depositInfo.staked || depositInfo.stake < MINIMUM_STAKE) {
  console.log("Staking factory...");

  // Call addStake with the minimum unstake delay (7 days)
  // This must be called FROM the factory address (msg.sender must be factory)
  // The factory contract needs to have a function that calls EntryPoint.addStake()
  const tx = await entryPoint.addStake(UNSTAKE_DELAY_SEC, {
    value: MINIMUM_STAKE,
    from: FACTORY_ADDRESS, // This must be called by the factory itself
  });

  await tx.wait();
  console.log("Factory staked successfully!");
} else {
  console.log("Factory is already staked with sufficient amount");
}
```

### Method 2: Direct Contract Call (Factory Must Call EntryPoint)

**Important**: `EntryPoint.addStake()` must be called **from the factory address** (`msg.sender` must be the factory). The factory contract needs to implement a function that calls `EntryPoint.addStake()`.

#### Option A: If Factory Has a Staking Function

If your `SimpleAccountFactory` has a function like `stakeWithEntryPoint()`, you can call it:

```typescript
const factoryABI = ["function stakeWithEntryPoint() payable"];
const factory = new ethers.Contract(FACTORY_ADDRESS, factoryABI, signer);

const tx = await factory.stakeWithEntryPoint({
  value: MINIMUM_STAKE,
});
await tx.wait();
```

#### Option B: Add Staking Function to Factory

If the factory doesn't have a staking function, you need to add one. The factory contract should include:

```solidity
// In SimpleAccountFactory.sol
function stakeWithEntryPoint() external payable {
    IEntryPoint(entryPoint()).addStake{value: msg.value}(604800); // 7 days
}
```

Then deploy the updated factory and call this function.

### Method 3: Using cast (Foundry)

```bash
# Set environment variables
export ENTRY_POINT=0x33860348ce61ea6cec276b1cf93c5465d1a92131
export FACTORY=0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f
export RPC_URL=https://testnet.evm.nodes.onflow.org
export PRIVATE_KEY=your_private_key_here

# Check current stake
cast call $ENTRY_POINT \
  "getDepositInfo(address)(uint256,bool,uint256,uint32,uint48)" \
  $FACTORY \
  --rpc-url $RPC_URL

# Stake factory (must be called FROM factory address)
# Note: This requires the factory to have a function that calls EntryPoint.addStake()
cast send $FACTORY \
  "stakeWithEntryPoint()" \
  --value 1000000000000000000000 \
  --private-key $PRIVATE_KEY \
  --rpc-url $RPC_URL
```

## Verification

After staking, verify the stake:

```typescript
const depositInfo = await entryPoint.getDepositInfo(FACTORY_ADDRESS);
console.log("Stake amount:", ethers.formatEther(depositInfo.stake), "FLOW");
console.log("Is staked:", depositInfo.staked);
console.log(
  "Unstake delay:",
  depositInfo.unstakeDelaySec.toString(),
  "seconds"
);
```

Expected output:

- `stake >= 1000` FLOW (for testnet)
- `staked = true`
- `unstakeDelaySec >= 604800` (7 days)

## Important Notes

1. **Caller Must Be Factory**: `EntryPoint.addStake()` checks that `msg.sender` is the account being staked. This means:

   - You cannot call `EntryPoint.addStake()` directly from an EOA
   - The factory contract must call `EntryPoint.addStake()` itself
   - The factory needs a function that forwards the call to EntryPoint

2. **One-Time Setup**: Once staked, the factory remains staked until explicitly unstaked (after the unstake delay period).

3. **Unstake Delay**: The minimum unstake delay is 7 days (604,800 seconds). Once you stake, you cannot unstake immediately - you must wait the delay period.

4. **Stake Amount**: The minimum stake for testnet is 1,000 FLOW. You can stake more if desired.

## Troubleshooting

### Error: "Not staked" or "Stake too low"

- Ensure the factory has called `EntryPoint.addStake()` with sufficient value
- Check that `msg.sender` was the factory address when `addStake()` was called
- Verify the stake amount meets the minimum (1,000 FLOW for testnet)

### Error: "Cannot call from EOA"

- `EntryPoint.addStake()` must be called by the factory contract itself
- Add a function to the factory that calls `EntryPoint.addStake()`
- Call that factory function instead of calling EntryPoint directly

### Checking Stake Status

```bash
# Using cast
cast call 0x33860348ce61ea6cec276b1cf93c5465d1a92131 \
  "getDepositInfo(address)(uint256,bool,uint256,uint32,uint48)" \
  0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f \
  --rpc-url https://testnet.evm.nodes.onflow.org
```

## References

- EntryPoint v0.9.0 Contract: `0x33860348ce61ea6cec276b1cf93c5465d1a92131`
- SimpleAccountFactory: `0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f`
- ERC-4337 Specification: https://eips.ethereum.org/EIPS/eip-4337
- EntryPoint Source: https://github.com/eth-infinitism/account-abstraction
