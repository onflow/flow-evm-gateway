# UserOp Hash Calculation Fix

## Critical Bug Found

**Issue**: Gateway and client were calculating different UserOp hashes, causing signature validation to fail.

**Client Hash**: `0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82`
**Gateway Hash (before fix)**: `0x5410e0bc5fd05128a4a13880f2c4b9ee73d50082cc09a2534afd7528de72bbcf`

## Root Cause

The gateway's hash calculation was **incorrect** for EntryPoint v0.9.0 format.

### Incorrect Implementation (Before Fix)

The gateway was doing:
1. Pack UserOp fields **including** entryPoint and chainID in the packed data
2. Hash the entire packed data: `keccak256(packedData)`

This was wrong because EntryPoint v0.9.0 requires a two-step hash.

### Correct Implementation (After Fix)

EntryPoint v0.9.0 format:
1. Pack **only** UserOp fields (without entryPoint and chainID)
2. Hash the packed UserOp: `firstHash = keccak256(packedUserOp)`
3. Pack: `firstHash || entryPoint || chainId`
4. Final hash: `finalHash = keccak256(firstHash || entryPoint || chainId)`

## Code Changes

### `models/user_operation.go`

**Before**:
```go
func (uo *UserOperation) Hash(entryPoint common.Address, chainID *big.Int) (common.Hash, error) {
    packed, err := uo.PackForSignature(entryPoint, chainID)  // Included entryPoint and chainID
    return crypto.Keccak256Hash(packed), nil
}

func (uo *UserOperation) PackForSignature(entryPoint common.Address, chainID *big.Int) ([]byte, error) {
    // Packed entryPoint and chainID at the end
    packed = append(packed, chainIDBytes...)
    packed = append(packed, entryPoint.Bytes()...)
    return packed, nil
}
```

**After**:
```go
func (uo *UserOperation) Hash(entryPoint common.Address, chainID *big.Int) (common.Hash, error) {
    // Pack only UserOp fields
    packedUserOp, err := uo.PackForSignature()
    if err != nil {
        return common.Hash{}, err
    }

    // Hash the packed UserOp
    packedUserOpHash := crypto.Keccak256Hash(packedUserOp)

    // Pack: keccak256(packedUserOp) || entryPoint || chainId
    var finalPacked []byte
    finalPacked = append(finalPacked, packedUserOpHash.Bytes()...)
    finalPacked = append(finalPacked, entryPoint.Bytes()...)
    
    chainIDBytes := make([]byte, 32)
    if chainID != nil {
        chainID.FillBytes(chainIDBytes)
    }
    finalPacked = append(finalPacked, chainIDBytes...)

    // Final hash
    return crypto.Keccak256Hash(finalPacked), nil
}

func (uo *UserOperation) PackForSignature() ([]byte, error) {
    // Pack only UserOp fields (no entryPoint or chainID)
    // ... (same packing logic, but without entryPoint and chainID)
    return packed, nil
}
```

## EntryPoint v0.9.0 Hash Format

The correct format is:
```
userOpHash = keccak256(
    keccak256(packedUserOp) || entryPoint || chainId
)
```

Where `packedUserOp` is:
```
encodePacked([
    sender,                    // 20 bytes
    nonce,                     // 32 bytes
    keccak256(initCode),       // 32 bytes
    keccak256(callData),       // 32 bytes
    callGasLimit,              // 32 bytes
    verificationGasLimit,       // 32 bytes
    preVerificationGas,         // 32 bytes
    maxFeePerGas,              // 32 bytes
    maxPriorityFeePerGas,       // 32 bytes
    keccak256(paymasterAndData) // 32 bytes
])
```

## Testing

After this fix:
- Gateway hash should match client hash: `0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82`
- Signature validation should pass
- EntryPoint validation should succeed

## Impact

This fix resolves the root cause of the validation failure. The signature was valid, but the gateway was validating it against the wrong hash.

## Next Steps

1. Deploy this fix
2. Test with the same UserOp
3. Verify hash matches client calculation
4. Confirm signature validation passes

