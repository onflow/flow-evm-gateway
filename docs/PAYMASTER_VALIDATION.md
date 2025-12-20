# Paymaster Signature Validation

## Overview

Paymaster signature validation in ERC-4337 allows paymasters to sign UserOperations to authorize gas sponsorship. The validation process involves both on-chain (in the paymaster contract) and off-chain (in the bundler) components.

## Standard Implementations

### 1. OpenZeppelin Paymasters (✅ Selected Implementation)

**We use OpenZeppelin's PaymasterERC20 as the standard paymaster implementation.**

OpenZeppelin provides several paymaster implementations:

- **`PaymasterERC20`**: Allows users to pay gas with ERC-20 tokens ✅ **Selected**
- **`PaymasterERC20Guarantor`**: Allows a third party (guarantor) to back user operations
- **`SignatureBasedPaymaster`**: Base contract for signature-based validation (not used for PaymasterERC20)

**Reference**: https://docs.openzeppelin.com/community-contracts/0.0.1/paymasters

**Deployment Guide**: See `docs/OPENZEPPELIN_PAYMASTER.md` for complete deployment instructions.

### 2. Coinbase VerifyingPaymaster

Coinbase's `VerifyingPaymaster` is a production-ready implementation that:
- Accepts signatures for validation
- Supports optional prechecks
- Can restrict sponsorship to certain bundlers
- Works with EntryPoint v0.7+

**Reference**: https://github.com/coinbase/verifying-paymaster

### 3. EntryPoint v0.6 vs v0.9

**EntryPoint v0.6** (Current target):
- Paymaster signatures are **optional** and handled entirely by the paymaster contract
- The `paymasterAndData` field format is paymaster-specific
- No standard signature marker

**EntryPoint v0.9+**:
- Introduced `PAYMASTER_SIG_MAGIC` marker for standardized signature format
- Signature appended to `paymasterAndData` with magic marker
- Signature not included in UserOperation hash calculation

## PaymasterAndData Format

### EntryPoint v0.6 (Current)

The `paymasterAndData` field format is paymaster-specific. Common patterns:

```
paymasterAndData = paymasterAddress (20 bytes) + [optional data]
```

For signature-based paymasters:
```
paymasterAndData = paymasterAddress (20 bytes) + signature (65 bytes) + [optional extra data]
```

### EntryPoint v0.9+

```
paymasterAndData = paymasterAddress (20 bytes) + [data] + PAYMASTER_SIG_MAGIC (1 byte) + signature (65 bytes)
```

Where `PAYMASTER_SIG_MAGIC = 0x01` (or similar marker defined by EntryPoint).

## On-Chain Validation

The paymaster contract implements `validatePaymasterUserOp()`:

```solidity
function validatePaymasterUserOp(
    UserOperation calldata userOp,
    bytes32 userOpHash,
    uint256 maxCost
) external override returns (bytes memory context) {
    // 1. Extract signature from paymasterAndData
    // 2. Verify signature against userOpHash (or custom hash)
    // 3. Check paymaster deposit
    // 4. Perform any additional validation
    // 5. Return context for postOp
}
```

## Off-Chain Validation (Bundler)

The bundler should validate paymaster signatures **before** submitting UserOperations to ensure:
1. The signature is valid
2. The paymaster will accept the operation
3. The paymaster has sufficient deposit

### Current Implementation Status

Our current implementation in `services/requester/userop_validator.go`:
- ✅ Extracts paymaster address from `paymasterAndData`
- ✅ Checks paymaster deposit via `EntryPoint.getDeposit()`
- ✅ **OpenZeppelin PaymasterERC20 support** - Parses and validates format
- ✅ **No signature validation needed** - OpenZeppelin PaymasterERC20 uses token-based validation only
- ⚠️ Other paymaster types (VerifyingPaymaster) - relies on `simulateValidation` to catch invalid signatures

### Recommended Implementation

For EntryPoint v0.6, signature validation depends on the paymaster contract design. Common approaches:

#### 1. ECDSA Signature Validation

```go
// Extract signature from paymasterAndData (after 20-byte address)
if len(userOp.PaymasterAndData) >= 85 { // 20 (address) + 65 (signature)
    paymasterAddr := common.BytesToAddress(userOp.PaymasterAndData[:20])
    signature := userOp.PaymasterAndData[20:85]
    
    // Recover signer from signature
    // Note: The hash to sign depends on paymaster implementation
    // Common: keccak256(abi.encodePacked(userOpHash, maxCost, paymasterAddress))
    hash := crypto.Keccak256Hash(
        userOpHash.Bytes(),
        maxCost.Bytes(),
        paymasterAddr.Bytes(),
    )
    
    pubkey, err := crypto.SigToPub(hash.Bytes(), signature)
    if err != nil {
        return fmt.Errorf("invalid signature: %w", err)
    }
    
    signer := crypto.PubkeyToAddress(*pubkey)
    // Verify signer is authorized (check against paymaster's authorized signers)
    // This requires calling the paymaster contract or maintaining an allowlist
}
```

#### 2. VerifyingPaymaster Pattern (Coinbase)

For paymasters following Coinbase's VerifyingPaymaster pattern:

```go
// VerifyingPaymaster uses a specific hash format
// hash = keccak256(abi.encodePacked(
//     userOp.sender,
//     userOp.nonce,
//     keccak256(userOp.callData),
//     userOp.callGasLimit,
//     userOp.verificationGasLimit,
//     userOp.preVerificationGas,
//     userOp.maxFeePerGas,
//     userOp.maxPriorityFeePerGas,
//     block.chainid,
//     paymasterAddress
// ))
```

#### 3. OpenZeppelin PaymasterERC20 Pattern

OpenZeppelin paymasters typically include:
- Token address
- Token price
- Validation data

The signature format is paymaster-specific.

## Implementation Recommendations

### For EntryPoint v0.6

1. **Basic Validation** (Current - ✅ Implemented):
   - Extract paymaster address
   - Check deposit via `EntryPoint.getDeposit()`
   - Rely on `simulateValidation` for signature validation

2. **Enhanced Validation** (Recommended):
   - For known paymaster contracts, implement signature validation
   - Maintain a registry of paymaster types and their validation logic
   - Support common patterns (VerifyingPaymaster, PaymasterERC20, etc.)

3. **Generic Validation** (Advanced):
   - Call paymaster's `validatePaymasterUserOp` via `eth_call` to simulate
   - This is expensive but works for any paymaster

### For EntryPoint v0.9+

1. **Standard Signature Format**:
   - Look for `PAYMASTER_SIG_MAGIC` marker
   - Extract signature after marker
   - Validate using standard hash format

## Example: VerifyingPaymaster Validation

```go
func validateVerifyingPaymasterSignature(
    userOp *models.UserOperation,
    userOpHash common.Hash,
    chainID *big.Int,
    paymasterAddr common.Address,
) error {
    // Extract signature (assuming VerifyingPaymaster format)
    if len(userOp.PaymasterAndData) < 85 {
        return fmt.Errorf("paymasterAndData too short for signature")
    }
    
    signature := userOp.PaymasterAndData[20:85]
    
    // Calculate hash according to VerifyingPaymaster spec
    hash := calculateVerifyingPaymasterHash(userOp, chainID, paymasterAddr)
    
    // Recover signer
    pubkey, err := crypto.SigToPub(hash.Bytes(), signature)
    if err != nil {
        return fmt.Errorf("invalid signature: %w", err)
    }
    
    signer := crypto.PubkeyToAddress(*pubkey)
    
    // Verify signer is authorized
    // This would require calling the paymaster contract or maintaining a registry
    // For now, we rely on simulateValidation
    
    return nil
}

func calculateVerifyingPaymasterHash(
    userOp *models.UserOperation,
    chainID *big.Int,
    paymasterAddr common.Address,
) common.Hash {
    // Implementation depends on VerifyingPaymaster version
    // This is a simplified example
    data := abiEncode(
        userOp.Sender,
        userOp.Nonce,
        crypto.Keccak256Hash(userOp.CallData),
        userOp.CallGasLimit,
        userOp.VerificationGasLimit,
        userOp.PreVerificationGas,
        userOp.MaxFeePerGas,
        userOp.MaxPriorityFeePerGas,
        chainID,
        paymasterAddr,
    )
    return crypto.Keccak256Hash(data)
}
```

## Current Status

Our implementation in `services/requester/userop_validator.go`:
- ✅ Basic paymaster validation (address extraction, deposit checking)
- ⚠️ Signature validation deferred to `simulateValidation`

This is **acceptable** for initial implementation because:
1. `simulateValidation` will catch invalid signatures on-chain
2. Paymaster signature formats vary by implementation
3. Full validation requires paymaster-specific logic

## Future Enhancements

1. **Paymaster Registry**: Maintain a registry of known paymaster contracts and their validation logic
2. **Signature Validation Library**: Implement common signature validation patterns
3. **Simulation-Based Validation**: Use `eth_call` to simulate `validatePaymasterUserOp` for unknown paymasters
4. **EntryPoint v0.9+ Support**: Add support for `PAYMASTER_SIG_MAGIC` when upgrading

## References

- [ERC-4337 Paymaster Documentation](https://docs.erc4337.io/paymasters/index.html)
- [OpenZeppelin Paymaster Contracts](https://docs.openzeppelin.com/community-contracts/0.0.1/paymasters)
- [Coinbase VerifyingPaymaster](https://github.com/coinbase/verifying-paymaster)
- [ERC-7562: Validation Rules](https://docs.erc4337.io/core-standards/erc-7562.html)

