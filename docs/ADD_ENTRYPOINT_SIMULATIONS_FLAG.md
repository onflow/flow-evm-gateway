# Where to Add --entry-point-simulations-address Flag

## Location: Systemd Service File

You need to add the flag to the **systemd service file** on your EC2 instance.

### File Path

```
/etc/systemd/system/flow-evm-gateway.service
```

**OR** (if using the deploy directory structure):

```
/etc/flow/systemd/flow-evm-gateway.service
```

### Exact Location

Add the flag to the `ExecStart` line in the `[Service]` section, after the other flags.

## Step-by-Step Instructions

### 1. SSH to Your EC2 Instance

```bash
ssh -i ~/Downloads/your-key.pem ec2-user@3.150.43.95
```

### 2. Edit the Service File

```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

**OR** if the file is in a different location:

```bash
# Find the file
sudo find /etc -name "flow-evm-gateway.service" 2>/dev/null

# Then edit it
sudo nano /path/to/flow-evm-gateway.service
```

### 3. Find the ExecStart Line

Look for the `ExecStart` line that looks like this:

```ini
ExecStart=docker run --rm \
	--name flow-evm-gateway \
	us-west1-docker.pkg.dev/dl-flow-devex-production/development/flow-evm-gateway:${VERSION} \
    --database-dir=/data \
    --access-node-grpc-host=${ACCESS_NODE_GRPC_HOST} \
    --flow-network-id=${FLOW_NETWORK_ID} \
    --init-cadence-height=${INIT_CADENCE_HEIGHT} \
    --coinbase=${COINBASE} \
    --coa-address=${COA_ADDRESS} \
    --coa-key=${COA_KEY} \
    --access-node-spork-hosts=${ACCESS_NODE_SPORK_HOSTS} \
    --ws-enabled=true \
    --tx-state-validation=local-index \
    --rate-limit=9999999 \
    --rpc-host=0.0.0.0 \
    --log-level=error
```

### 4. Add the Flag

Add `--entry-point-simulations-address=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3` to the end of the flags list:

```ini
ExecStart=docker run --rm \
	--name flow-evm-gateway \
	us-west1-docker.pkg.dev/dl-flow-devex-production/development/flow-evm-gateway:${VERSION} \
    --database-dir=/data \
    --access-node-grpc-host=${ACCESS_NODE_GRPC_HOST} \
    --flow-network-id=${FLOW_NETWORK_ID} \
    --init-cadence-height=${INIT_CADENCE_HEIGHT} \
    --coinbase=${COINBASE} \
    --coa-address=${COA_ADDRESS} \
    --coa-key=${COA_KEY} \
    --access-node-spork-hosts=${ACCESS_NODE_SPORK_HOSTS} \
    --ws-enabled=true \
    --tx-state-validation=local-index \
    --rate-limit=9999999 \
    --rpc-host=0.0.0.0 \
    --log-level=error \
    --entry-point-simulations-address=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

**Note**: Make sure to add a backslash `\` at the end of the previous line (`--log-level=error \`) if it doesn't already have one, and **no backslash** on the last line.

### 5. Also Add EntryPoint Address (if not already present)

If you don't see `--entry-point-address` in the flags, add it too:

```ini
    --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
    --entry-point-simulations-address=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

### 6. Save and Exit

- **Save**: `Ctrl+O`, then `Enter`
- **Exit**: `Ctrl+X`

### 7. Reload and Restart

```bash
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
sudo systemctl status flow-evm-gateway --no-pager
```

### 8. Verify the Flag is Being Used

Check the logs to confirm:

```bash
sudo journalctl -u flow-evm-gateway -n 20 --no-pager | grep -i "simulations\|EntryPoint"
```

You should see logs indicating the simulations contract is being used:

```json
{
  "level": "debug",
  "entryPoint": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
  "simulationsAddress": "0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3",
  "message": "using EntryPointSimulations contract for simulateValidation (v0.7+)"
}
```

## Alternative: Using Environment Variable (if supported)

If your service file uses environment variables, you could also add it to `/etc/flow/runtime-conf.env`:

```bash
sudo nano /etc/flow/runtime-conf.env
```

Add:

```
ENTRY_POINT_SIMULATIONS_ADDRESS=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
```

Then update the service file to use it:

```ini
    --entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS}
```

But the **direct flag approach** (adding it directly to ExecStart) is simpler and more reliable.

## Quick Reference

- **Flag**: `--entry-point-simulations-address=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`
- **File**: `/etc/systemd/system/flow-evm-gateway.service` (or wherever your service file is)
- **Section**: `[Service]` → `ExecStart` line
- **After editing**: `sudo systemctl daemon-reload && sudo systemctl restart flow-evm-gateway`

