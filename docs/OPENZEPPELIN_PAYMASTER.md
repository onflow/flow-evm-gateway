# OpenZeppelin Paymaster Implementation Guide

## Overview

The EVM Gateway uses **OpenZeppelin's PaymasterERC20** as the standard paymaster implementation. This allows users to pay gas fees using ERC-20 tokens instead of native currency (FLOW).

## OpenZeppelin Paymaster Contracts

OpenZeppelin provides several paymaster implementations:

1. **`PaymasterERC20`**: Base contract for ERC-20 token-based gas payments
2. **`PaymasterERC20Guarantor`**: Allows a third party (guarantor) to back user operations
3. **`SignatureBasedPaymaster`**: Base for signature-based validation (not used for PaymasterERC20)

**Reference**: https://docs.openzeppelin.com/community-contracts/0.0.1/paymasters

## PaymasterAndData Format

For OpenZeppelin `PaymasterERC20`, the `paymasterAndData` field format is:

```
paymasterAndData = paymasterAddress (20 bytes) + tokenAddress (20 bytes) + validationData (variable)
```

Where:
- **paymasterAddress**: The deployed PaymasterERC20 contract address
- **tokenAddress**: The ERC-20 token address users will pay with
- **validationData**: Additional data (token price, exchange rate, etc.) encoded by the paymaster

### Example

```go
// paymasterAndData structure
paymasterAddr := common.HexToAddress("0x1234...") // 20 bytes
tokenAddr := common.HexToAddress("0x5678...")      // 20 bytes
validationData := []byte{...}                      // Variable length

paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)
paymasterAndData = append(paymasterAndData, validationData...)
```

## Implementation Details

### Code Support

The gateway includes support for OpenZeppelin PaymasterERC20 in:
- `services/requester/openzeppelin_paymaster.go`: Parsing and validation
- `services/requester/userop_validator.go`: Integration with UserOp validation

### Validation Process

1. **Format Validation**: Parse `paymasterAndData` to extract paymaster address, token address, and validation data
2. **Deposit Check**: Verify paymaster has sufficient deposit in EntryPoint
3. **On-Chain Validation**: Rely on `simulateValidation` to check:
   - User's token balance
   - Token price/exchange rate
   - Paymaster's willingness to sponsor

**Note**: OpenZeppelin PaymasterERC20 does **not** use signatures. Validation is based on token balances and prices.

## Deployment Guide

### Prerequisites

1. **EntryPoint Contract**: Must be deployed first (see EntryPoint deployment guide)
2. **ERC-20 Token**: Token contract that users will pay with
3. **Deployer Account**: Account with sufficient FLOW for deployment and initial deposit

### Step 1: Install OpenZeppelin Contracts

```bash
npm install @openzeppelin/contracts-account
# or
yarn add @openzeppelin/contracts-account
```

### Step 2: Create Paymaster Contract

Create a contract extending OpenZeppelin's `PaymasterERC20`:

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts-account/paymaster/PaymasterERC20.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

contract MyPaymasterERC20 is PaymasterERC20 {
    constructor(
        IEntryPoint _entryPoint,
        IERC20 _token,
        address _owner
    ) PaymasterERC20(_entryPoint, _token, _owner) {
        // Additional initialization if needed
    }

    // Override _fetchDetails if you need custom validation data encoding
    function _fetchDetails(
        UserOperation calldata userOp
    ) internal view override returns (
        bytes memory context,
        IERC20 token,
        uint256 price
    ) {
        // Extract token address and price from paymasterAndData
        // This is called during validatePaymasterUserOp
        address tokenAddress = address(bytes20(userOp.paymasterAndData[20:40]));
        token = IERC20(tokenAddress);
        
        // Decode price from validationData (custom encoding)
        // price = decodePrice(userOp.paymasterAndData[40:]);
        
        // Return context for postOp
        context = abi.encode(userOp.sender, tokenAddress);
    }
}
```

### Step 3: Deploy Paymaster Contract

#### Using Hardhat

```javascript
// scripts/deploy-paymaster.js
const { ethers } = require("hardhat");

async function main() {
  const [deployer] = await ethers.getSigners();
  
  const ENTRY_POINT_ADDRESS = "0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"; // EntryPoint v0.6
  const TOKEN_ADDRESS = "0x..."; // Your ERC-20 token address
  
  const PaymasterERC20 = await ethers.getContractFactory("MyPaymasterERC20");
  const paymaster = await PaymasterERC20.deploy(
    ENTRY_POINT_ADDRESS,
    TOKEN_ADDRESS,
    deployer.address // owner
  );
  
  await paymaster.deployed();
  console.log("Paymaster deployed to:", paymaster.address);
}
```

#### Using Foundry

```solidity
// script/DeployPaymaster.s.sol
pragma solidity ^0.8.20;

import {Script} from "forge-std/Script.sol";
import {MyPaymasterERC20} from "../src/MyPaymasterERC20.sol";

contract DeployPaymaster is Script {
    function run() external {
        address entryPoint = vm.envAddress("ENTRY_POINT_ADDRESS");
        address token = vm.envAddress("TOKEN_ADDRESS");
        
        vm.startBroadcast();
        MyPaymasterERC20 paymaster = new MyPaymasterERC20(
            IEntryPoint(entryPoint),
            IERC20(token),
            msg.sender
        );
        vm.stopBroadcast();
        
        console.log("Paymaster deployed to:", address(paymaster));
    }
}
```

### Step 4: Deposit to EntryPoint

The paymaster must deposit FLOW (native currency) into the EntryPoint contract to cover gas costs:

```javascript
// Deposit FLOW to EntryPoint for paymaster
const entryPoint = await ethers.getContractAt("IEntryPoint", ENTRY_POINT_ADDRESS);
const depositAmount = ethers.utils.parseEther("1.0"); // 1 FLOW

await entryPoint.depositTo(paymaster.address, {
  value: depositAmount
});

console.log("Deposited", depositAmount.toString(), "to paymaster");
```

### Step 5: Configure Gateway

Update gateway configuration with the deployed paymaster address:

```go
// config/config.go or runtime config
EntryPointAddress: common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"),
BundlerEnabled: true,
// Paymaster addresses are discovered from UserOperations
```

## Testing

### Test Paymaster Functionality

1. **Deploy Test Token**:
```solidity
// Simple ERC-20 for testing
contract TestToken is ERC20 {
    constructor() ERC20("Test Token", "TEST") {
        _mint(msg.sender, 1000000 * 10**18);
    }
}
```

2. **Create UserOperation with Paymaster**:
```javascript
const userOp = {
  sender: smartAccountAddress,
  nonce: 0,
  callData: encodedCallData,
  paymasterAndData: ethers.utils.concat([
    paymasterAddress,  // 20 bytes
    tokenAddress,     // 20 bytes
    validationData    // Variable
  ]),
  // ... other fields
};
```

3. **Submit to Gateway**:
```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "eth_sendUserOperation",
    "params": [userOp, entryPointAddress],
    "id": 1
  }'
```

## PaymasterERC20Guarantor

For scenarios where a third party (guarantor) backs user operations:

```solidity
contract MyPaymasterERC20Guarantor is PaymasterERC20Guarantor {
    constructor(
        IEntryPoint _entryPoint,
        IERC20 _token,
        address _owner
    ) PaymasterERC20Guarantor(_entryPoint, _token, _owner) {}
    
    function _fetchGuarantor(
        UserOperation calldata userOp
    ) internal view override returns (address) {
        // Extract guarantor address from paymasterAndData
        return address(bytes20(userOp.paymasterAndData[40:60]));
    }
}
```

## Security Considerations

1. **Deposit Management**: Monitor paymaster deposits and top up as needed
2. **Token Price**: Ensure token price/oracle is secure and accurate
3. **Rate Limiting**: Implement rate limiting to prevent abuse
4. **Access Control**: Use OpenZeppelin's access control for owner functions
5. **Reentrancy**: OpenZeppelin contracts include reentrancy guards

## Monitoring

Monitor the following metrics:
- Paymaster deposit balance
- Number of sponsored transactions
- Token price/exchange rate
- Failed validations (insufficient balance, invalid price, etc.)

## References

- [OpenZeppelin Paymaster Documentation](https://docs.openzeppelin.com/community-contracts/0.0.1/paymasters)
- [OpenZeppelin Contracts Account GitHub](https://github.com/OpenZeppelin/openzeppelin-contracts-account)
- [ERC-4337 Paymaster Guide](https://docs.erc4337.io/paymasters/index.html)

