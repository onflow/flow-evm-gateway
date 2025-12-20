# Diagnose Key Loading Issue

## ✅ Confirmed: Keys Match

The test script confirms your `COA_KEY` matches key index 0's public key. All 101 keys should be loaded.

## Diagnostic Steps

### Step 1: Check Gateway Configuration on Server

SSH into your server and check the runtime config:

```bash
# Check COA configuration
sudo cat /etc/flow/runtime-conf.env | grep -iE "COA_KEY|COA_CLOUD_KMS|COA_ADDRESS"

# Verify the COA_KEY value matches your local test
# It should be: cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38
```

### Step 2: Check Gateway Startup Logs

Look for key loading messages in the startup logs:

```bash
# Check recent startup logs for keystore activity
sudo journalctl -u flow-evm-gateway --since '1 hour ago' | grep -iE "keystore|signer|bootstrap|COA|key" | head -50

# Or check the full startup sequence
sudo journalctl -u flow-evm-gateway --since '1 hour ago' --no-pager | tail -100
```

Look for:
- Messages about fetching COA account
- Messages about creating signer
- Messages about loading keys
- Any errors related to keys or signer

### Step 3: Check Current Gateway Status

```bash
# Check if gateway is running
sudo systemctl status flow-evm-gateway

# Check for any recent errors
sudo journalctl -u flow-evm-gateway --since '10 minutes ago' --priority=err
```

### Step 4: Check if Keys Are Actually Loaded

If the gateway has an API endpoint to check available keys, use it. Otherwise, try submitting a UserOp and watch the logs:

```bash
# Watch logs in real-time while submitting a UserOp
sudo journalctl -u flow-evm-gateway -f | grep -iE "keystore|signing.*key|no.*key|available.*key"
```

### Step 5: Verify Gateway Was Restarted After Adding Keys

```bash
# Check when the gateway was last started
sudo systemctl show flow-evm-gateway -p ActiveEnterTimestamp

# Check when keys were added (if you have that info)
# The gateway must be restarted after adding keys for them to be loaded
```

## Common Issues

### Issue 1: Gateway Not Restarted
**Symptom**: Keys were added but gateway wasn't restarted  
**Solution**: Restart the gateway:
```bash
sudo systemctl restart flow-evm-gateway
```

### Issue 2: COA_KEY Mismatch in Config
**Symptom**: The `COA_KEY` in `/etc/flow/runtime-conf.env` doesn't match your test  
**Solution**: Update the config file with the correct key and restart

### Issue 3: IndexOnly Mode
**Symptom**: Gateway is running in index-only mode  
**Solution**: Check if `--index-only` flag is set (keys won't load in this mode)

### Issue 4: All Keys Locked
**Symptom**: Keys are loaded but all are in use  
**Solution**: Wait for transactions to complete or check for stuck transactions

## Next Steps

1. **Run the diagnostic commands above on your server**
2. **Share the results** - especially:
   - The COA_KEY value from the config
   - Any startup logs about key loading
   - Any errors in the logs

This will help identify why keys aren't being loaded despite the match.

