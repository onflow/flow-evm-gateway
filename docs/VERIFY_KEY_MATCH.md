# Verify Signing Key Match

## Problem

Gateway still shows "no signing keys available" even after adding 100 keys and restarting.

## Root Cause

The gateway only loads keys that match the signer's public key. If the keys you added don't match the gateway's configured signer, they won't be loaded.

## Diagnosis Steps

### Step 1: Check Gateway Configuration

```bash
# Check what key the gateway is configured with
sudo cat /etc/flow/runtime-conf.env | grep -iE "COA_KEY|COA_CLOUD_KMS|COA_ADDRESS"
```

This shows:
- `COA_ADDRESS`: The Flow account address
- `COA_KEY`: The private key (if using direct key)
- `COA_CLOUD_KMS_*`: KMS configuration (if using KMS)

### Step 2: Get Public Key from Gateway's Signer

If using `COA_KEY`:
- The public key is derived from the private key
- You need to extract it

If using `COA_CLOUD_KMS`:
- The public key comes from KMS
- You need to query KMS or check the account

### Step 3: Verify Key Match

The public key from Step 2 must match the public key you used to add the 100 keys (from key index 0).

## Solution

### If Keys Don't Match

You have two options:

#### Option A: Add Keys Using Gateway's Public Key

1. Get the gateway's public key (from COA_KEY or KMS)
2. Use that public key to add new keys:

```cadence
transaction {
  prepare(signer: auth(AddKey) &Account) {
    // Get the gateway's public key (you need to provide this)
    let gatewayPublicKey: PublicKey = // ... gateway's public key
    
    let range: InclusiveRange<Int> = InclusiveRange(1, 100, step: 1)
    for element in range {
      signer.keys.add(
        publicKey: gatewayPublicKey,
        hashAlgorithm: HashAlgorithm.SHA2_256,
        weight: 1000.0
      )
    }
  }
}
```

#### Option B: Reconfigure Gateway to Use Matching Key

1. Get the public key from key index 0 (the one you used to add keys)
2. Get the corresponding private key
3. Update gateway's `COA_KEY` to use that private key
4. Restart gateway

## Quick Check: Verify Account Has Keys

```bash
# Use Flow CLI to check the account
flow accounts get <COA_ADDRESS>
```

You should see 101 keys listed. But the gateway will only use the ones that match its signer's public key.

## Expected Behavior After Fix

Once keys match:
1. Gateway starts successfully
2. Keystore loads matching keys
3. Bundler can submit transactions
4. No more "no signing keys available" errors

