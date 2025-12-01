# Signing Keys Not Loading After Adding

## Problem

After adding 100 signing keys to the COA account, the gateway still reports "no signing keys available" after restart.

## Root Cause

The gateway only loads keys that **match the signer's public key**. If the keys you added don't match the signer configured in the gateway, they won't be loaded.

## Diagnosis

### Check 1: Verify Key Matching

The gateway loads keys in `bootstrap/bootstrap.go`:

```go
signer, err := createSigner(ctx, b.config, b.logger)
for _, key := range account.Keys {
    // Skip account keys that do not use the same Public Key as the
    // configured crypto.Signer object.
    if !key.PublicKey.Equals(signer.PublicKey()) {
        continue  // Key is skipped!
    }
    accountKeys = append(accountKeys, ...)
}
```

**The issue**: Only keys matching `signer.PublicKey()` are added to the keystore.

### Check 2: Verify Which Public Key Gateway Uses

Check what public key the gateway is configured with:

```bash
# Check COA_KEY or COA_CLOUD_KMS configuration
sudo cat /etc/flow/runtime-conf.env | grep -iE "COA_KEY|COA_CLOUD_KMS"
```

### Check 3: Verify Which Public Key Was Used to Add Keys

From your transaction, the keys were added using `firstKey.publicKey` from key index 0. You need to verify:

1. **What public key is key index 0?** (The one you copied)
2. **What public key is the gateway using?** (From COA_KEY or COA_CLOUD_KMS)

These must match!

### Check 4: Check Startup Logs for Key Loading

```bash
# Check for errors during key loading
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -iE "COA|signer|account.*key|failed.*get.*account|failed.*create.*signer" | head -30
```

Look for:
- Errors getting COA account
- Errors creating signer
- Warnings about no matching keys

## Solution

### Option 1: Verify Key Match (Most Likely Issue)

The public key used to add the keys must match the public key the gateway is using.

1. **Get the public key from key index 0** (the one you used to add keys)
2. **Get the public key from COA_KEY or COA_CLOUD_KMS** (what gateway uses)
3. **They must be the same!**

If they don't match:
- Either add keys using the gateway's public key, OR
- Configure the gateway to use the public key you used to add keys

### Option 2: Check Account Key Count

Verify the account actually has the keys:

```bash
# Use Flow CLI to check account
flow accounts get <COA_ADDRESS>
```

You should see 101 keys (index 0-100).

### Option 3: Check Signer Creation

The gateway creates a signer from either:
- `COA_KEY` (private key) → derives public key
- `COA_CLOUD_KMS` → gets public key from KMS

Make sure the public key from this signer matches the public key of the keys you added.

## Quick Test

To verify the issue, check startup logs for how many keys were loaded:

```bash
# Check if any keys were loaded at startup
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -iE "keystore|signing.*key|account.*key" | head -20
```

If you see no logs about keys being loaded, or if you see errors, that's the issue.

## Expected Behavior

After restart with matching keys, you should see:
- No errors about getting COA account
- No errors about creating signer
- Keys being loaded into keystore
- Bundler successfully submitting transactions

## Most Common Issue

**The public key used to add the 100 keys doesn't match the public key the gateway is configured with.**

Fix: Either:
1. Add keys using the gateway's public key, OR
2. Reconfigure the gateway to use the public key you used to add keys

