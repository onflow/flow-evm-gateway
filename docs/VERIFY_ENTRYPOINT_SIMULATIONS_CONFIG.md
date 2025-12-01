# How to Verify EntryPointSimulations Configuration

## Problem

The error shows the gateway is still calling EntryPoint (`0xCf1e8398747A05a997E8c964E957e47209bdFF08`) instead of EntryPointSimulations (`0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`).

## Diagnostic Steps

### 1. Check Environment Variable is Set

```bash
# On EC2, check the environment file
sudo cat /etc/flow/runtime-conf.env | grep ENTRY_POINT_SIMULATIONS_ADDRESS
```

**Expected output:**
```
ENTRY_POINT_SIMULATIONS_ADDRESS=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

**If missing or wrong:** Edit the file:
```bash
sudo nano /etc/flow/runtime-conf.env
# Add or fix:
ENTRY_POINT_SIMULATIONS_ADDRESS=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

### 2. Check Service File Has the Flag

```bash
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep entry-point-simulations
```

**Expected output:**
```
--entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS}
```

**If missing:** Add it after `--log-level=info`:
```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

Add:
```ini
    --log-level=info \
    --entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS}
```

### 3. Check Gateway Logs at Startup

After restarting, check the logs for the configuration message:

```bash
sudo journalctl -u flow-evm-gateway -n 50 --no-pager | grep -i "EntryPointSimulations\|entryPointSimulationsAddress"
```

**Expected output (if configured correctly):**
```json
{
  "level": "info",
  "component": "userop-validator",
  "entryPointAddress": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "entryPointSimulationsAddress": "0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3",
  "message": "EntryPointSimulations configured - will use for simulateValidation calls"
}
```

**If you see "not configured":**
```json
{
  "level": "info",
  "component": "userop-validator",
  "entryPointAddress": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "message": "EntryPointSimulations not configured - will use EntryPoint for simulateValidation (v0.6 compatibility)"
}
```

This means the config is not being read. Check steps 1 and 2.

### 4. Check Logs During UserOperation

When you send a UserOperation, check for:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -E "simulationAddress|EntryPointSimulations|using EntryPointSimulations"
```

**Expected (if configured correctly):**
```json
{
  "level": "info",
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "entryPointSimulationsAddress": "0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3",
  "simulationAddress": "0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3",
  "message": "using EntryPointSimulations contract for simulateValidation (v0.7+)"
}
```

**If you see EntryPoint address in simulationAddress:**
```json
{
  "simulationAddress": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "message": "using EntryPoint contract for simulateValidation (v0.6 compatibility mode) - EntryPointSimulations not configured"
}
```

This confirms the config is not being read.

## Common Issues

### Issue 1: Environment Variable Not Loaded

**Symptom:** Gateway logs show "not configured" even though you added it to the file.

**Fix:**
1. Make sure the service file has `EnvironmentFile=/etc/flow/runtime-conf.env`
2. Restart the service: `sudo systemctl daemon-reload && sudo systemctl restart flow-evm-gateway`
3. Check if the variable is actually being loaded by the service

### Issue 2: Flag Not in Service File

**Symptom:** Environment variable is set but gateway doesn't use it.

**Fix:**
1. Add `--entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS}` to the service file
2. Make sure it's after `--log-level=info` with a backslash `\` on the previous line
3. Restart: `sudo systemctl daemon-reload && sudo systemctl restart flow-evm-gateway`

### Issue 3: Wrong Address Format

**Symptom:** Gateway shows an error about invalid address.

**Fix:**
- Make sure the address has `0x` prefix: `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`
- Check for typos
- Verify it's exactly 42 characters (including `0x`)

## Quick Verification Command

Run this to check everything at once:

```bash
echo "=== Environment Variable ==="
sudo cat /etc/flow/runtime-conf.env | grep ENTRY_POINT_SIMULATIONS_ADDRESS

echo -e "\n=== Service File Flag ==="
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep entry-point-simulations

echo -e "\n=== Gateway Startup Logs ==="
sudo journalctl -u flow-evm-gateway -n 100 --no-pager | grep -i "EntryPointSimulations\|entryPointSimulationsAddress" | tail -5
```

## After Fixing

1. **Restart the service:**
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart flow-evm-gateway
   ```

2. **Verify in logs:**
   ```bash
   sudo journalctl -u flow-evm-gateway -n 20 --no-pager | grep -i "EntryPointSimulations"
   ```

3. **Test with a UserOperation** and check logs show `simulationAddress` as `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`

