# EntryPoint v0.9.0 Implementation Summary

## Overview

This document summarizes the implementation of EntryPoint v0.9.0 support in the Flow EVM Gateway, based on the TDD plan for EntryPoint v0.9.0 simulation architecture and validation behavior.

## Implementation Status

### ✅ Completed

#### 1. Simulation Architecture (Section 1 of TDD Plan)

**Status**: ✅ Complete

- **State Override Implementation**: Gateway uses `eth_call` with state override to temporarily replace EntryPoint's code with EntryPointSimulations bytecode
  - State override format: Geth-style third parameter to `eth_call`
  - Bytecode: Embedded `EntryPointSimulationsDeployedBytecode` from `services/abis/EntryPointSimulations.bytecode`
  - Location: `services/requester/userop_validator.go:simulateValidation()`

- **Direct EntryPoint Calls**: Gateway calls `EntryPoint.simulateValidation` directly (recommended approach)
  - Falls back to separately deployed `EntryPointSimulations` only if configured (legacy mode)
  - Configuration: `EntryPointSimulationsAddress` (optional)

**Test Coverage**:
- ✅ Test 1.1.1: `simulateValidation` is callable via state override
- ✅ Test 1.1.2: `simulateValidation` runs EntryPointSimulations code (via state override)

#### 2. ValidationResult Handling (Section 1.1 of TDD Plan)

**Status**: ✅ Complete

- **EntryPoint v0.9.0 Behavior**: `simulateValidation` returns `ValidationResult` normally on success (not via revert)
  - Success: `eth_call` succeeds, return data contains ABI-encoded `ValidationResult`
  - Failure: `eth_call` reverts with `FailedOp`, `PaymasterNotDeployed`, etc.

- **Struct Decoding**: Implemented `DecodeValidationResult()` function
  - Decodes `ReturnInfo`, `StakeInfo`, `AggregatorStakeInfo` from ABI-encoded return data
  - Location: `services/requester/entrypoint_abi.go`
  - Uses EntryPointSimulations ABI for decoding

**Code Changes**:
- `services/requester/entrypoint_abi.go`: Added `ValidationResult`, `ReturnInfo`, `StakeInfo`, `AggregatorStakeInfo` structs
- `services/requester/userop_validator.go`: Updated `simulateValidation()` to handle successful returns

#### 3. ValidationData Interpretation (Section 3 of TDD Plan)

**Status**: ✅ Complete

- **Decoding**: Implemented `DecodeValidationData()` function
  - Format: `aggregatorOrSigFail` (160 bits) | `validUntil` (48 bits) << 160 | `validAfter` (48 bits) << 208
  - Location: `services/requester/userop_validator.go`

- **Sentinel Values** (from EntryPoint v0.9.0):
  - `SIG_VALIDATION_SUCCESS = 0`: No aggregator, signature OK
  - `SIG_VALIDATION_FAILED = 1`: No aggregator, signature failed
  - `aggregatorOrSigFail > 1`: Actual aggregator address

- **Validation Checks**:
  - ✅ Test 3.1: Signature failure detection (rejects UserOp if `aggregatorOrSigFail == 1`)
  - ✅ Test 3.2: Account validity window check (`validAfter <= validUntil`)
  - ✅ Test 3.3: Paymaster validity window check
- ✅ Block-range handling: Implements block-range flag (`VALIDITY_BLOCK_RANGE_FLAG = 1 << 47`, mask = flag-1)
  - If both validAfter and validUntil have the flag set → interpret as block numbers; else timestamps
  - Compares against current block number/timestamp (uses indexed block)
  - Skips comparison (logs) if block indexer unavailable

**Code Changes**:
- `services/requester/userop_validator.go`: Added `ValidationData` struct and `DecodeValidationData()` function
- `services/requester/userop_validator.go`: Implemented signature failure and validity window checks in `validateValidationResult()`

**TODO**:
- ⚠️ Current time/block comparison: Already implemented using indexed block; consider adding tolerance/clock-skew handling if needed

#### 4. Stake Checks (Section 4 of TDD Plan)

**Status**: ✅ Complete

- **Configuration**: Added stake minimum fields to `Config` struct
  - `MinSenderStake *big.Int`
  - `MinFactoryStake *big.Int`
  - `MinPaymasterStake *big.Int`
  - `MinAggregatorStake *big.Int`
  - `MinUnstakeDelaySec uint64` (default: 7 days = 604800 seconds)

- **Default Values** (dollar-equivalent approach):
  - **Production** (mainnet):
    - Sender/Factory: 3,300 FLOW (~$330, equivalent to 0.1 ETH)
    - Paymaster/Aggregator: 33,000 FLOW (~$3,300, equivalent to 1 ETH)
  - **Testnet** (testnet/emulator/previewnet):
    - Sender/Factory: 1,000 FLOW (~$100)
    - Paymaster/Aggregator: 10,000 FLOW (~$1,000)

- **Validation Checks**:
  - ✅ Test 4.1: Sender stake threshold check
  - ✅ Test 4.2: Paymaster stake check (when paymaster is used)
  - ✅ Test 4.3: Aggregator stake check (when aggregator is used)
  - ✅ Factory stake check (when initCode is present)

**Code Changes**:
- `config/config.go`: Added stake fields and `SetDefaultStakeRequirements()` function
- `services/requester/userop_validator.go`: Implemented stake validation in `validateValidationResult()`

#### 5. Prefund Calculation Verification (Test 2.2)

**Status**: ✅ Complete

- **Verification**: Validates that `returnInfo.prefund` matches expected calculation according to EntryPoint v0.9.0 `_getRequiredPrefund`
  - Full formula: `(preVerificationGas + callGasLimit + verificationGasLimit + paymasterVerificationGasLimit + paymasterPostOpGasLimit) * maxFeePerGas`
  - **Without paymaster**: Verifies exact match (all values are integers, no rounding needed)
    - Formula: `(preVerificationGas + callGasLimit + verificationGasLimit) * maxFeePerGas`
    - Treats mismatch as critical error (gateway bug or EntryPoint version mismatch)
  - **With paymaster**: Verifies that prefund is at least the base amount
    - Base: `(preVerificationGas + callGasLimit + verificationGasLimit) * maxFeePerGas`
    - Actual prefund includes additional paymaster gas (`paymasterVerificationGasLimit + paymasterPostOpGasLimit`)
    - Note: Paymaster gas limits are computed by EntryPoint during paymaster validation and are not available in UserOperation struct
    - Logs the difference (paymaster gas) for debugging
  - Location: `services/requester/userop_validator.go:validateValidationResult()`

**Rationale**: This is a safety invariant to ensure gateway's understanding of gas math matches EntryPoint's. Mismatches indicate either a gateway bug or EntryPoint version mismatch, both of which should be caught immediately.

#### 6. UserOp Hash Verification (Test 2.1)

**Status**: ✅ Complete (previously implemented)

- Gateway calls `EntryPoint.getUserOpHash()` to get authoritative hash
- No manual hash calculation in production code
- Location: `services/requester/requester.go:GetUserOpHash()`

#### 7. Error Handling

**Status**: ✅ Complete

- **Account Deployment (Test 2.3)**: Handles `AA20 account not deployed` errors
- **Paymaster Not Deployed (Test 2.4)**: Handles `PaymasterNotDeployed` errors
- **FailedOp Decoding**: Decodes `FailedOp(uint256,string)` and `FailedOpWithRevert(uint256,string,bytes)` errors
- **AA Error Codes**: Extracts and logs AAxx error codes (AA13, AA20, AA23, etc.)

### ⏭️ Skipped (Future Work)

#### 1. Banned Opcodes and Storage Access Checks (Section 5 of TDD Plan)

**Status**: ⏭️ Skipped for now

**Reason**: Requires trace analysis capabilities that need further investigation for Flow EVM.

**What's Needed**:
- **Trace Analysis**: Gateway must obtain execution trace (e.g., via `debug_traceCall` or equivalent)
- **Banned Opcode Detection**: Verify no banned opcodes were executed by account/paymaster code during validation
- **Storage Access Validation**: Verify no unauthorized storage slots outside the account's "owned state" were accessed

**Requirements**:
- Exact banned opcode list from ERC-4337 spec
- Formal definition of "outside the account's data" storage ranges
- Flow EVM support for `debug_traceCall` or equivalent tracing API
- Mapping of "account's data" to storage ranges in Flow EVM

**Test Coverage** (not yet implemented):
- ❌ Test 5.1: Banned opcode detection
- ❌ Test 5.2: Storage access outside allowed ranges

**Future Implementation**:
1. Verify Flow EVM supports `debug_traceCall` or equivalent
2. Obtain exact banned opcode list from ERC-4337 spec
3. Define storage access rules for Flow EVM
4. Implement trace analysis and validation

## Configuration

### EntryPoint Configuration

```go
// EntryPoint address (required when bundler is enabled)
EntryPointAddress common.Address

// EntryPointSimulations address (optional, legacy mode)
// If not set, gateway calls EntryPoint.simulateValidation directly (recommended)
EntryPointSimulationsAddress common.Address
```

### Stake Requirements Configuration

Stake requirements are automatically set based on network type via `SetDefaultStakeRequirements()`:

- **Production** (mainnet): Higher values matching Ethereum mainnet economics
- **Testnet** (testnet/emulator/previewnet): Lower values for easier testing

Values can be overridden by setting the config fields directly before creating the validator.

## Code Structure

### Key Files

1. **`services/requester/entrypoint_abi.go`**:
   - ValidationResult structs (`ReturnInfo`, `StakeInfo`, `AggregatorStakeInfo`, `ValidationResult`)
   - `DecodeValidationResult()` function
   - ABI encoding/decoding functions

2. **`services/requester/userop_validator.go`**:
   - `simulateValidation()`: Handles EntryPoint v0.9.0 successful returns
   - `validateValidationResult()`: Implements validation pipeline (validationData, stake checks, prefund verification)
   - `DecodeValidationData()`: Decodes validationData with exact sentinel values

3. **`config/config.go`**:
   - Stake requirement fields
   - `SetDefaultStakeRequirements()`: Sets defaults based on network type

4. **`services/abis/EntryPointSimulations.bytecode`**:
   - Embedded deployed bytecode for state override

## Validation Pipeline

The gateway's validation pipeline for EntryPoint v0.9.0 (Section 6 of TDD Plan):

1. **Stateless Checks**: JSON schema validation, global limits
2. **Compute userOpHash**: Call `EntryPoint.getUserOpHash()` via `eth_call`
3. **Run simulateValidation**: Use state override with EntryPointSimulations bytecode
4. **Handle Response**:
   - **Success**: Decode `ValidationResult` from return data
   - **Failure**: Decode revert data as `FailedOp`/`PaymasterNotDeployed`/etc.
5. **Validate ValidationResult**:
   - Interpret validationData (signature failure, time windows)
   - Check stake values (sender, factory, paymaster, aggregator)
   - Verify prefund calculation
6. **On Success**: Insert UserOp into mempool

## Testing Status

### Implemented Tests

- ✅ Test 1.1.1: `simulateValidation` must be callable
- ✅ Test 1.1.2: `simulateValidation` runs EntryPointSimulations code
- ✅ Test 2.1: userOpHash matches EntryPoint.getUserOpHash
- ✅ Test 2.2: Prefund calculation verification
- ✅ Test 2.3: Account deployment / AA20 path
- ✅ Test 2.4: Paymaster not deployed / PaymasterNotDeployed
- ✅ Test 3.1: Signature failure (account)
- ✅ Test 3.2: Expired validity window
- ✅ Test 3.3: Paymaster validity
- ✅ Test 4.1: Sender stake threshold
- ✅ Test 4.2: Paymaster stake
- ✅ Test 4.3: Aggregator stake
- ✅ Test 6.1: Fully valid operation is accepted
- ✅ Test 6.2: Every failure mode is surfaced correctly

### Not Yet Implemented

- ❌ Test 5.1: Banned opcode detection
- ❌ Test 5.2: Storage access outside allowed ranges

## Known Limitations

1. **Block Range Flags**: Validity window checks don't yet handle block range flags (timestamp vs block number)
2. **Current Time Comparison**: Validity window checks don't yet compare against current block timestamp/number
3. **Banned Opcodes**: Not yet implemented (requires trace analysis)
4. **Storage Access**: Not yet implemented (requires trace analysis)

## Future Work

1. **Implement Block Range Flag Handling**: Support for timestamp vs block number in validity windows
2. **Add Current Time Comparison**: Check validity windows against current block timestamp/number
3. **Implement Banned Opcode Detection**: Add trace analysis for banned opcode detection
4. **Implement Storage Access Validation**: Add trace analysis for unauthorized storage access detection
5. **Add CLI Flags**: Optional command-line flags to override default stake requirements

## References

- EntryPoint v0.9.0: https://github.com/eth-infinitism/account-abstraction
- ERC-4337 Specification: https://eips.ethereum.org/EIPS/eip-4337
- TDD Plan: See conversation history for detailed technical plan

