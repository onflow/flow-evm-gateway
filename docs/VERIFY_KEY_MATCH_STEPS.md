# How to Verify COA Key Match

## Your Values

- **COA_ADDRESS**: `0xdd4a4464762431db`
- **COA_KEY** (private key): `cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38`

## Step 1: Get Public Key from COA Account (Key Index 0)

### Using Flow CLI

```bash
# Get account info and extract key index 0's public key
flow accounts get 0xdd4a4464762431db --network testnet
```

Look for key index 0 in the output and note its public key.

### Using Flowscan or Block Explorer

1. Go to: https://evm-testnet.flowscan.io/address/0xdd4a4464762431db
2. View the account details
3. Find key index 0 and note its public key

### Using RPC Call (if available)

```bash
curl -X POST https://rest-testnet.onflow.org/v1/accounts/0xdd4a4464762431db \
  -H 'Content-Type: application/json' | jq '.keys[0].publicKey'
```

## Step 2: Derive Public Key from COA_KEY (Private Key)

### Option A: Using Flow CLI

```bash
# Create a temporary file with the private key
echo "cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38" > /tmp/coa-key.txt

# Use Flow CLI to get the public key (if it supports this)
# Note: Flow CLI might not have a direct command for this
```

### Option B: Using a Simple Script

Create a script to derive the public key:

```bash
# Save this as get-public-key.sh
#!/bin/bash
PRIVATE_KEY="cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38"

# For ECDSA_P256 (Flow's default)
# You'll need to use a tool that can derive public key from private key
# Flow CLI or a simple Go/Node script
```

### Option C: Quick Test - Check if Gateway Can See Keys

The easiest way is to check if the gateway loaded any keys at startup. If it loaded keys, they match. If it loaded 0 keys, they don't match.

## Step 3: Compare the Public Keys

Once you have both public keys:
1. **Key Index 0's public key** (from account)
2. **Public key from COA_KEY** (derived from private key)

They must be **exactly the same** for the gateway to load the keys.

## Quick Verification Method

The simplest way to verify is to check if the gateway loaded keys:

```bash
# Check startup logs for any indication of keys being loaded
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -iE "keystore|signing.*key|account.*key|COA" | head -20
```

If you see errors or no indication of keys being loaded, they likely don't match.

## Alternative: Test by Adding Keys with Gateway's Public Key

If you can't easily verify, you can:

1. **Temporarily add logging** to see what public key the gateway derives
2. **Or add new keys** using a public key you know matches

## Using Flow CLI to Get Account Keys

```bash
# Make sure Flow CLI is configured for testnet
flow accounts get 0xdd4a4464762431db --network testnet

# The output will show all keys with their public keys
# Find key index 0 and note its public key
```

## Using a Simple Go Script (if you have Go installed)

You could create a simple Go program to:
1. Decode the private key from hex
2. Derive the public key
3. Print it for comparison

But the easiest is probably to use Flow CLI to get the account and see key index 0's public key.

