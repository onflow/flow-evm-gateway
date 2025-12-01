# handleOps ABI Encoding Fix

## Problem Identified

UserOperations were being accepted and added to the pool, but never included in blocks. The bundler was running and finding UserOps, but failing to create transactions with this error:

```
"failed to encode handleOps calldata: failed to encode handleOps: abi: cannot use []interface {} as type [0]struct as argument"
```

## Root Cause

The `EncodeHandleOps` function was using `[]interface{}` with anonymous structs:

```go
ops := make([]interface{}, len(userOps))
for i, userOp := range userOps {
    ops[i] = struct { ... } { ... }  // Anonymous struct
}
```

The Go ABI encoder (`abi.Pack`) cannot handle `[]interface{}` with anonymous structs - it requires a concrete, named struct type.

## Fix

Created a named struct type `UserOperationABI` and use it instead:

```go
type UserOperationABI struct {
    Sender               common.Address
    Nonce                *big.Int
    InitCode             []byte
    CallData             []byte
    CallGasLimit         *big.Int
    VerificationGasLimit *big.Int
    PreVerificationGas   *big.Int
    MaxFeePerGas         *big.Int
    MaxPriorityFeePerGas *big.Int
    PaymasterAndData     []byte
    Signature            []byte
}

func EncodeHandleOps(userOps []*models.UserOperation, beneficiary common.Address) ([]byte, error) {
    ops := make([]UserOperationABI, len(userOps))  // Concrete type
    for i, userOp := range userOps {
        ops[i] = UserOperationABI{ ... }  // Named struct
    }
    // ...
}
```

## Impact

- ✅ **Before**: Bundler found UserOps but failed to create transactions
- ✅ **After**: Bundler can successfully create `handleOps` transactions
- ✅ **Result**: UserOps will now be included in blocks

## Verification

Tests pass:
```bash
go test ./services/requester -run TestEncodeHandleOps -v
# PASS: TestEncodeHandleOps
```

## What to Expect After Deploy

1. UserOps are accepted (same as before)
2. Bundler finds UserOps (same as before)
3. **NEW**: Transactions are successfully created
4. **NEW**: Transactions are submitted to pool
5. **NEW**: UserOps are included in blocks

## Logs to Watch For

After deployment, you should see:
- ✅ `"created handleOps transaction for batch"` - Transaction creation starts
- ✅ `"created handleOps transaction"` with `txHash` - Transaction created successfully
- ✅ `"submitted bundled transaction to pool"` - Transaction submitted
- ❌ No more `"failed to encode handleOps calldata"` errors

## Version

- **New Version**: `testnet-v1-fix-handleops-encoding`
- **Previous Version**: `testnet-v1-entrypoint-simulations-fix`

