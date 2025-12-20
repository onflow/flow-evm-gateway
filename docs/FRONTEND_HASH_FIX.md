# Frontend UserOp Hash Calculation Fix

## Problem

The frontend is calculating a different UserOp hash than the gateway/EntryPoint, causing AA24 signature validation errors.

**Gateway Hash** (from EntryPoint): `0xfa42beb47ea25109887c22196fcfb89de692679f41778e465cca49440a5c0781`  
**Frontend Hash** (current): `0x7c3a2d03feb6a8b70ff29ef6e79d4e10ad1ec884f7983ce5228f7f7f0c6c5d8c`

**Status**: Frontend hash still doesn't match. The frontend must call `EntryPoint.getUserOpHash()` directly instead of manual calculation.

## Root Cause

The frontend is using an **old hash format** that doesn't match EntryPoint v0.9.0's **PackedUserOperation** format.

## Solution: Use EntryPoint.getUserOpHash() (Recommended)

**Best Practice**: Call `EntryPoint.getUserOpHash()` instead of manual calculation.

### Complete Working Example (ethers.js v6)

```typescript
import { ethers } from "ethers";

const ENTRY_POINT_ADDRESS = "0x33860348CE61eA6CeC276b1cF93C5465D1a92131";

// EntryPoint ABI - only need getUserOpHash function
const ENTRY_POINT_ABI = [
  {
    inputs: [
      {
        components: [
          { name: "sender", type: "address" },
          { name: "nonce", type: "uint256" },
          { name: "initCode", type: "bytes" },
          { name: "callData", type: "bytes" },
          { name: "accountGasLimits", type: "bytes32" },
          { name: "preVerificationGas", type: "uint256" },
          { name: "gasFees", type: "bytes32" },
          { name: "paymasterAndData", type: "bytes" },
          { name: "signature", type: "bytes" },
        ],
        internalType: "struct PackedUserOperation",
        name: "userOp",
        type: "tuple",
      },
    ],
    name: "getUserOpHash",
    outputs: [{ name: "", type: "bytes32" }],
    stateMutability: "view",
    type: "function",
  },
];

// Packing functions (must match gateway implementation exactly)
function packAccountGasLimits(
  callGasLimit: bigint,
  verificationGasLimit: bigint
): string {
  const result = new Uint8Array(32);

  // Pack verificationGasLimit into upper 16 bytes (bytes 0-15, right-aligned)
  const verificationGasBytes = toBytes16(verificationGasLimit);
  result.set(verificationGasBytes, 0);

  // Pack callGasLimit into lower 16 bytes (bytes 16-31, right-aligned)
  const callGasBytes = toBytes16(callGasLimit);
  result.set(callGasBytes, 16);

  return (
    "0x" +
    Array.from(result)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("")
  );
}

function packGasFees(
  maxFeePerGas: bigint,
  maxPriorityFeePerGas: bigint
): string {
  const result = new Uint8Array(32);

  // Pack maxPriorityFeePerGas into upper 16 bytes (bytes 0-15, right-aligned)
  const maxPriorityFeeBytes = toBytes16(maxPriorityFeePerGas);
  result.set(maxPriorityFeeBytes, 0);

  // Pack maxFeePerGas into lower 16 bytes (bytes 16-31, right-aligned)
  const maxFeeBytes = toBytes16(maxFeePerGas);
  result.set(maxFeeBytes, 16);

  return (
    "0x" +
    Array.from(result)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("")
  );
}

function toBytes16(value: bigint): Uint8Array {
  // Convert to 16-byte array, right-aligned (big-endian)
  const hex = value.toString(16).padStart(32, "0"); // Pad to 32 hex chars (16 bytes)
  const bytes = new Uint8Array(16);
  for (let i = 0; i < 16; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16);
  }
  return bytes;
}

// Get UserOp hash from EntryPoint
async function getUserOpHash(
  provider: ethers.Provider,
  userOp: {
    sender: string;
    nonce: bigint;
    initCode: string;
    callData: string;
    callGasLimit: bigint;
    verificationGasLimit: bigint;
    preVerificationGas: bigint;
    maxFeePerGas: bigint;
    maxPriorityFeePerGas: bigint;
    paymasterAndData: string;
  }
): Promise<string> {
  const entryPoint = new ethers.Contract(
    ENTRY_POINT_ADDRESS,
    ENTRY_POINT_ABI,
    provider
  );

  const packedUserOp = {
    sender: userOp.sender,
    nonce: userOp.nonce,
    initCode: userOp.initCode,
    callData: userOp.callData,
    accountGasLimits: packAccountGasLimits(
      userOp.callGasLimit,
      userOp.verificationGasLimit
    ),
    preVerificationGas: userOp.preVerificationGas,
    gasFees: packGasFees(userOp.maxFeePerGas, userOp.maxPriorityFeePerGas),
    paymasterAndData: userOp.paymasterAndData,
    signature: "0x", // EMPTY - hash is calculated WITHOUT signature
  };

  const hash = await entryPoint.getUserOpHash(packedUserOp);
  return hash;
}

// Usage
const userOpHash = await getUserOpHash(provider, userOp);
console.log("UserOp hash from EntryPoint:", userOpHash);
// Should match: 0xfa42beb47ea25109887c22196fcfb89de692679f41778e465cca49440a5c0781
```

### Verification

After calling `getUserOpHash`, verify it matches the gateway hash:

- **Expected hash**: `0xfa42beb47ea25109887c22196fcfb89de692679f41778e465cca49440a5c0781`
- If it doesn't match, check:
  1. Are you using the correct EntryPoint address? (`0x33860348CE61eA6CeC276b1cF93C5465D1a92131`)
  2. Is `signature` set to `"0x"` (empty)?
  3. Are the packing functions correct? (verificationGasLimit in bytes 0-15, callGasLimit in bytes 16-31)
  4. Are all values using `bigint` type (not `number` or `string`)?

## Alternative: Manual Calculation (Not Recommended)

If you must calculate manually, use the **PackedUserOperation** format:

### Packing Functions

**IMPORTANT**: The packing order is specific - higher value goes in upper 16 bytes, lower value in lower 16 bytes.

```typescript
function packAccountGasLimits(
  callGasLimit: bigint,
  verificationGasLimit: bigint
): string {
  // Pack into bytes32: verificationGasLimit (bytes 0-15) || callGasLimit (bytes 16-31)
  // Both are right-aligned within their 16-byte slots
  const result = new Uint8Array(32);

  // Pack verificationGasLimit into upper 16 bytes (bytes 0-15, right-aligned)
  const verificationGasBytes = toBytes16(verificationGasLimit);
  result.set(verificationGasBytes, 0);

  // Pack callGasLimit into lower 16 bytes (bytes 16-31, right-aligned)
  const callGasBytes = toBytes16(callGasLimit);
  result.set(callGasBytes, 16);

  return (
    "0x" +
    Array.from(result)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("")
  );
}

function packGasFees(
  maxFeePerGas: bigint,
  maxPriorityFeePerGas: bigint
): string {
  // Pack into bytes32: maxPriorityFeePerGas (bytes 0-15) || maxFeePerGas (bytes 16-31)
  // Both are right-aligned within their 16-byte slots
  const result = new Uint8Array(32);

  // Pack maxPriorityFeePerGas into upper 16 bytes (bytes 0-15, right-aligned)
  const maxPriorityFeeBytes = toBytes16(maxPriorityFeePerGas);
  result.set(maxPriorityFeeBytes, 0);

  // Pack maxFeePerGas into lower 16 bytes (bytes 16-31, right-aligned)
  const maxFeeBytes = toBytes16(maxFeePerGas);
  result.set(maxFeeBytes, 16);

  return (
    "0x" +
    Array.from(result)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("")
  );
}

function toBytes16(value: bigint): Uint8Array {
  // Convert to 16-byte array, right-aligned (big-endian)
  const hex = value.toString(16).padStart(32, "0"); // Pad to 32 hex chars (16 bytes)
  const bytes = new Uint8Array(16);
  for (let i = 0; i < 16; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16);
  }
  return bytes;
}
```

### Hash Calculation

```typescript
// Step 1: Pack UserOp fields (PackedUserOperation format)
const packedUserOp = abi.encodePacked(
  [
    "address",
    "uint256",
    "bytes",
    "bytes",
    "bytes32",
    "uint256",
    "bytes32",
    "bytes",
  ],
  [
    userOp.sender,
    userOp.nonce,
    userOp.initCode,
    userOp.callData,
    packAccountGasLimits(userOp.callGasLimit, userOp.verificationGasLimit),
    userOp.preVerificationGas,
    packGasFees(userOp.maxFeePerGas, userOp.maxPriorityFeePerGas),
    userOp.paymasterAndData,
  ]
);

// Step 2: Hash the packed UserOp
const packedUserOpHash = keccak256(packedUserOp);

// Step 3: Pack: keccak256(packedUserOp) || entryPoint || chainId
const finalPacked = abi.encodePacked(
  ["bytes32", "address", "uint256"],
  [packedUserOpHash, ENTRY_POINT_ADDRESS, CHAIN_ID]
);

// Step 4: Final hash
const userOpHash = keccak256(finalPacked);
```

## EntryPoint v0.9.0 PackedUserOperation Format

The PackedUserOperation struct is:

```solidity
struct PackedUserOperation {
    address sender;              // 20 bytes
    uint256 nonce;               // 32 bytes
    bytes initCode;              // variable length
    bytes callData;              // variable length
    bytes32 accountGasLimits;    // 32 bytes: callGasLimit (16 bytes) || verificationGasLimit (16 bytes)
    uint256 preVerificationGas;   // 32 bytes
    bytes32 gasFees;             // 32 bytes: maxFeePerGas (16 bytes) || maxPriorityFeePerGas (16 bytes)
    bytes paymasterAndData;      // variable length
    bytes signature;             // variable length (excluded from hash)
}
```

## Important Notes

1. **Signature is EXCLUDED**: The hash is calculated with an **empty signature** (`0x` or `[]`). The signature signs the hash, so the hash cannot include the signature.

2. **Gas Values are Packed**:

   - `callGasLimit` and `verificationGasLimit` → `accountGasLimits` (bytes32)
   - `maxFeePerGas` and `maxPriorityFeePerGas` → `gasFees` (bytes32)

3. **Two-Step Hash**:

   - First: `keccak256(packedUserOp)`
   - Then: `keccak256(firstHash || entryPoint || chainId)`

4. **EntryPoint Address**: Use `0x33860348CE61eA6CeC276b1cF93C5465D1a92131` (Flow Testnet v0.9.0)

5. **Chain ID**: Use `545` (Flow Testnet)

## Verification

After fixing the frontend hash calculation:

- Frontend hash should match gateway hash: `0xfa42beb47ea25109887c22196fcfb89de692679f41778e465cca49440a5c0781`
- Signature validation should pass
- AA24 errors should disappear

## Gateway Status

✅ **Gateway is correct** - it uses `EntryPoint.getUserOpHash()` which is the authoritative source.  
❌ **Frontend needs to be fixed** - it's using an incorrect manual calculation.
