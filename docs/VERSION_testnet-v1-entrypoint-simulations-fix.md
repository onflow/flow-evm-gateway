# Version: testnet-v1-entrypoint-simulations-fix

## Summary

This version fixes the EntryPointSimulations integration by removing the unreliable bytecode selector check that was preventing calls to the EntryPointSimulations contract.

## Changes Made

### 1. Removed Bytecode Selector Check

**File**: `services/requester/userop_validator.go`

- **Removed**: Pre-call bytecode check that searched for function selector
- **Reason**: Bytecode optimization makes selector search unreliable. The contract has the function, but the selector may not be directly searchable in optimized bytecode.
- **Impact**: Gateway now directly calls `simulateValidation` on EntryPointSimulations without pre-checking

### 2. Simplified Empty Revert Handling

**File**: `services/requester/userop_validator.go`

- **Changed**: Empty revert error handling no longer assumes function doesn't exist
- **Before**: Checked bytecode for selector, failed if not found
- **After**: Logs empty revert with context but doesn't assume function is missing
- **Impact**: More accurate error messages, allows function calls to proceed

### 3. Updated Systemd Service File Template

**File**: `deploy/systemd-docker/flow-evm-gateway.service`

- **Added**: Required flags for EntryPointSimulations support:
  - `--entry-point-address=${ENTRY_POINT_ADDRESS}`
  - `--entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS}`
  - `--bundler-enabled=${BUNDLER_ENABLED}`

### 4. Updated Deployment Instructions

**File**: `docs/REDEPLOY_INSTRUCTIONS.md`

- **Updated**: Version to `testnet-v1-entrypoint-simulations-fix`
- **Added**: Configuration verification steps
- **Added**: Service file flag verification
- **Added**: Enhanced logging checks for EntryPointSimulations configuration

## Configuration Requirements

### Environment Variables (in `/etc/flow/runtime-conf.env`)

```bash
VERSION=testnet-v1-entrypoint-simulations-fix
ENTRY_POINT_ADDRESS=0xcf1e8398747a05a997e8c964e957e47209bdff08
ENTRY_POINT_SIMULATIONS_ADDRESS=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
BUNDLER_ENABLED=true
```

### Service File Flags

The systemd service file must include:
```ini
--entry-point-address=${ENTRY_POINT_ADDRESS} \
--entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS} \
--bundler-enabled=${BUNDLER_ENABLED}
```

## Verification

After deployment, verify the configuration:

```bash
# Check logs for EntryPointSimulations configuration
sudo journalctl -u flow-evm-gateway -n 20 --no-pager | grep -E "EntryPointSimulations|entryPointSimulationsAddress"

# Expected output:
# "EntryPointSimulations configured - will use for simulateValidation calls"
# "entryPointSimulationsAddress":"0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3"
```

## What This Fixes

1. **EntryPointSimulations calls now work**: Gateway properly calls `simulateValidation` on EntryPointSimulations contract
2. **No false negatives**: Removed unreliable bytecode check that was incorrectly flagging valid functions as missing
3. **Better error handling**: Empty reverts are logged with context but don't prevent function calls

## Testing

After deployment, test with a UserOperation:

```bash
# Monitor logs during UserOp submission
sudo journalctl -u flow-evm-gateway -f | grep -E "simulationAddress|EntryPointSimulations|simulateValidation"
```

**Expected**: Logs should show:
- `"simulationAddress":"0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3"`
- `"using EntryPointSimulations contract for simulateValidation (v0.7+)"`

## Rollback

If issues occur, rollback to previous version:

```bash
sudo nano /etc/flow/runtime-conf.env
# Change VERSION back to: testnet-v1-raw-initcode-logging
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
```

