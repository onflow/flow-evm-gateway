# EntryPointSimulations Support - Implementation Complete

## Summary

Updated the gateway to support EntryPoint v0.7+ by using the separate `EntryPointSimulations` contract for simulation calls, while keeping `EntryPoint` for actual execution.

## Changes Made

### 1. Added EntryPointSimulations Configuration

**File**: `config/config.go`

Added new config field:
```go
// EntryPointSimulationsAddress is the address of the EntryPointSimulations contract
// For EntryPoint v0.7+, simulation methods (simulateValidation) were moved to a separate contract
// Flow Testnet EntryPointSimulations: 0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
// If not set, gateway will attempt to use EntryPoint address (for backwards compatibility with v0.6)
EntryPointSimulationsAddress common.Address
```

### 2. Added Command-Line Flag

**File**: `cmd/run/cmd.go`

Added new flag:
```bash
--entry-point-simulations-address
```

**Usage:**
```bash
--entry-point-simulations-address=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

### 3. Updated simulateValidation Function

**File**: `services/requester/userop_validator.go`

**Key Changes:**
- Uses `EntryPointSimulations` address if configured, otherwise falls back to `EntryPoint` (v0.6 compatibility)
- Calls `simulateValidation` on the correct contract based on configuration
- Enhanced error messages to indicate which contract is being used
- Updated bytecode checking to verify selector exists in the simulation contract

**Logic:**
```go
// Determine which contract to call for simulation
simulationAddress := entryPoint
if v.config.EntryPointSimulationsAddress != (common.Address{}) {
    simulationAddress = v.config.EntryPointSimulationsAddress
    // Use EntryPointSimulations for v0.7+
} else {
    // Use EntryPoint for v0.6 compatibility
}
```

## Deployment Configuration

### Required Configuration

Add the `EntryPointSimulations` address to your gateway startup command:

```bash
--entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
--entry-point-simulations-address=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

### Contract Addresses (Flow Testnet)

- **EntryPoint**: `0xcf1e8398747a05a997e8c964e957e47209bdff08`
- **EntryPointSimulations**: `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`

## Backwards Compatibility

✅ **Backwards Compatible**: If `--entry-point-simulations-address` is not set, the gateway will:
- Attempt to use `EntryPoint` address for simulation (v0.6 behavior)
- Log a warning if simulation fails
- This allows gradual migration

## What This Fixes

1. ✅ **Empty Revert Issue**: Gateway now calls `simulateValidation` on the correct contract
2. ✅ **EntryPoint v0.9 Support**: Properly supports v0.7+ EntryPoints that use separate simulation contract
3. ✅ **Clear Error Messages**: Logs indicate which contract is being used for simulation
4. ✅ **Function Existence Check**: Verifies selector exists in the simulation contract bytecode

## Expected Behavior After Deployment

### Success Case

When `EntryPointSimulations` is configured correctly:

```json
{
  "level": "debug",
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "simulationsAddress": "0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3",
  "message": "using EntryPointSimulations contract for simulateValidation (v0.7+)"
}
```

Then simulation should succeed with proper revert data (ValidationResult or FailedOp).

### If Still Failing

If simulation still fails after configuring `EntryPointSimulations`:

1. **Verify EntryPointSimulations is deployed** at the configured address
2. **Check logs** for `"simulationAddress"` to confirm correct contract is being called
3. **Verify selector exists** in EntryPointSimulations bytecode
4. **Check revert data** - should now have non-empty data (ValidationResult or FailedOp)

## Testing

After deployment:

1. **Send a UserOperation**
2. **Check logs** for:
   - `"using EntryPointSimulations contract for simulateValidation (v0.7+)"`
   - `"simulationAddress": "0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3"`
   - Non-empty `revertReasonHex` (should have data now)
   - `isValidationResult: true` or `isFailedOp: true` with proper AA error codes

## Next Steps

1. ✅ **Code updated** - Gateway now supports EntryPointSimulations
2. **Deploy with new flag** - Add `--entry-point-simulations-address` to startup command
3. **Test UserOperation** - Verify simulation works correctly
4. **Monitor logs** - Confirm correct contract is being used and revert data is decoded

