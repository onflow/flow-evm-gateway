# Signing Keys Issue - "no signing keys available"

## Problem

The bundler successfully creates transactions, but submission fails with:
```
"error":"no signing keys available"
```

## Root Cause

The transaction pool needs signing keys from the COA (Cadence Owned Account) to sign Flow transactions that wrap the EVM transactions. If no keys are available, transactions cannot be submitted.

## Diagnosis

### Check 1: Is IndexOnly Enabled?

```bash
# Check service file
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep -i "index-only\|indexOnly"

# Check environment file
sudo cat /etc/flow/runtime-conf.env | grep -i "INDEX_ONLY"
```

**If `--index-only=true` or `INDEX_ONLY=true`**: This is the problem. Index-only mode doesn't create signing keys because it's not supposed to submit transactions.

**Solution**: Remove `--index-only` flag or set `INDEX_ONLY=false` if you want the bundler to work.

### Check 2: COA Account Configuration

```bash
# Check COA address and key configuration
sudo cat /etc/flow/runtime-conf.env | grep -iE "COA_ADDRESS|COA_KEY|COA_CLOUD_KMS"
```

**Required**:
- `COA_ADDRESS` must be set
- Either `COA_KEY` OR `COA_CLOUD_KMS` must be set

### Check 3: Check Startup Logs

```bash
# Check if keys were loaded at startup
sudo journalctl -u flow-evm-gateway --since "1 hour ago" | grep -iE "signing.*key|account.*key|keystore|COA" | head -20
```

Look for:
- Errors about getting COA account
- Errors about creating signer
- Warnings about no keys matching

### Check 4: Check Available Keys Metric

```bash
# Check metrics for available signing keys
curl http://localhost:9090/metrics 2>/dev/null | grep -i "signing.*key\|available.*key" || echo "Metrics not available"
```

## Solutions

### Solution 1: Disable Index-Only Mode

If `IndexOnly` is enabled, disable it:

1. Edit `/etc/flow/runtime-conf.env`:
   ```bash
   # Remove or comment out:
   # INDEX_ONLY=true
   ```

2. Or remove from service file:
   ```bash
   # Remove --index-only flag from ExecStart
   ```

3. Restart service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart flow-evm-gateway
   ```

### Solution 2: Verify COA Account Has Keys

The COA account must have keys that match the configured signer:

1. **Check COA account keys**:
   ```bash
   # Use Flow CLI or check on Flowscan
   flow accounts get <COA_ADDRESS>
   ```

2. **Verify key matches signer**: The public key of the COA account key must match the public key of the configured signer (from `COA_KEY` or `COA_CLOUD_KMS`).

3. **Add keys if needed**: If the COA account doesn't have matching keys, add them using Flow CLI or the account management tools.

### Solution 3: Add More Keys to COA Account

If all keys are locked/in use, add more keys to the COA account:

1. **Add additional keys** to the COA account
2. **Restart the gateway** so it picks up the new keys

### Solution 4: Check Key Locking

If keys are locked and not being released:

1. **Check for stuck transactions**: Look for transactions that never completed
2. **Check key release logic**: Verify `NotifyBlock` and `NotifyTransaction` are being called
3. **Restart service**: This will reset all key locks

## Verification

After fixing, verify keys are available:

```bash
# Check startup logs for key count
sudo journalctl -u flow-evm-gateway --since "1 minute ago" | grep -iE "signing.*key|available.*key|keystore"

# Submit a UserOp and watch for success
sudo journalctl -u flow-evm-gateway -f | grep -E "submitted bundled transaction|no signing keys"
```

**Expected**: Should see `"submitted bundled transaction"` instead of `"no signing keys available"`.

## Configuration Reference

The keystore is created in `bootstrap/bootstrap.go`:

```go
if !b.config.IndexOnly {
    // Get COA account
    account, err := b.client.GetAccount(ctx, b.config.COAAddress)
    // Create signer from COA_KEY or COA_CLOUD_KMS
    signer, err := createSigner(ctx, b.config, b.logger)
    // Match account keys to signer public key
    for _, key := range account.Keys {
        if key.PublicKey.Equals(signer.PublicKey()) {
            accountKeys = append(accountKeys, ...)
        }
    }
}
b.keystore = keystore.New(ctx, accountKeys, ...)
```

**Key Points**:
- If `IndexOnly=true`, `accountKeys` will be empty
- Only keys matching the signer's public key are added
- If no keys match, keystore will have 0 keys

