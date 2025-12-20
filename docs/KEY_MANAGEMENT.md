# Key Management Best Practices

## Overview

The EVM Gateway requires a COA (Contract Owned Account) private key for signing transactions. This document outlines safe practices for managing this sensitive key.

## Security Warning

⚠️ **NEVER commit private keys to version control!** Always use `.gitignore` to exclude files containing keys.

## Options for Providing COA Key

### Option 1: Environment Variables (Recommended for Development)

Use shell environment variables to avoid exposing keys in command history or process lists:

```bash
# Set environment variable
export COA_KEY="your-64-character-hex-private-key"

# Run gateway with environment variable
./evm-gateway run \
  --coa-address=<your-coa-address> \
  --coa-key="$COA_KEY" \
  # ... other flags
```

**Using `.env` file (with shell script):**

Create a `.env` file (make sure it's in `.gitignore`):

```bash
# .env
COA_KEY=your-64-character-hex-private-key
COA_ADDRESS=your-16-character-hex-address
COINBASE=your-evm-coinbase-address
```

Then source it before running:

```bash
# Load environment variables
source .env

# Run gateway
./evm-gateway run \
  --coa-address="$COA_ADDRESS" \
  --coa-key="$COA_KEY" \
  --coinbase="$COINBASE" \
  # ... other flags
```

Or use a wrapper script:

```bash
#!/bin/bash
# run-gateway.sh
set -a  # automatically export all variables
source .env
set +a
./evm-gateway run \
  --coa-address="$COA_ADDRESS" \
  --coa-key="$COA_KEY" \
  --coinbase="$COINBASE" \
  "$@"
```

### Option 2: Key File (Safer than Command Line)

Use the `--coa-key-file` option to read from a file:

```bash
# Create a secure key file (restrict permissions!)
echo "your-64-character-hex-private-key" > .coa-key
chmod 600 .coa-key  # Only owner can read/write

# Run gateway
./evm-gateway run \
  --coa-address=<your-coa-address> \
  --coa-key-file=.coa-key \
  # ... other flags
```

**Note**: The `--coa-key-file` option expects a JSON file format for key rotation. For a single key, you may need to check the exact format required.

### Option 3: Cloud KMS (Recommended for Production)

For production deployments, use Cloud KMS (Google Cloud or AWS) instead of storing keys directly:

```bash
./evm-gateway run \
  --coa-address=<your-coa-address> \
  --coa-cloud-kms-project-id=your-project-id \
  --coa-cloud-kms-location-id=global \
  --coa-cloud-kms-key-ring-id=tx-signing \
  --coa-cloud-kms-key=gw-key-1@1 \
  # ... other flags
```

**Advantages:**
- Keys never stored on disk
- Hardware security module (HSM) protection
- Audit logging
- Key rotation support
- No risk of key exposure in logs or process lists

## Setting Up `.env` File

### Step 1: Create `.env` File

```bash
# Create .env file
cat > .env << EOF
# Flow COA Configuration
COA_ADDRESS=your-16-character-hex-address
COA_KEY=your-64-character-hex-private-key

# EVM Configuration
COINBASE=your-evm-coinbase-address

# Network Configuration
FLOW_NETWORK_ID=flow-testnet
ACCESS_NODE_GRPC_HOST=access.testnet.nodes.onflow.org:9000
EOF
```

### Step 2: Secure the File

```bash
# Restrict file permissions (only owner can read/write)
chmod 600 .env

# Verify permissions
ls -la .env
# Should show: -rw------- (600)
```

### Step 3: Add to `.gitignore`

Create or update `.gitignore`:

```bash
# Sensitive configuration files
.env
.env.local
.env.*.local
*.key
.coa-key
coa-key.json
```

### Step 4: Create `.env.example` Template

Create a template file (safe to commit):

```bash
# .env.example
# Copy this file to .env and fill in your actual values
# DO NOT commit .env to version control!

# Flow COA Configuration
COA_ADDRESS=your-16-character-hex-address
COA_KEY=your-64-character-hex-private-key

# EVM Configuration
COINBASE=your-evm-coinbase-address

# Network Configuration
FLOW_NETWORK_ID=flow-testnet
ACCESS_NODE_GRPC_HOST=access.testnet.nodes.onflow.org:9000
```

## Complete Example

### Development Setup

```bash
# 1. Create .env file
cat > .env << EOF
COA_ADDRESS=0123456789abcdef
COA_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
COINBASE=0x3cC530e139Dd93641c3F30217B20163EF8b17159
FLOW_NETWORK_ID=flow-testnet
ACCESS_NODE_GRPC_HOST=access.testnet.nodes.onflow.org:9000
EOF

# 2. Secure it
chmod 600 .env

# 3. Add to .gitignore
echo ".env" >> .gitignore

# 4. Source and run
source .env
./evm-gateway run \
  --flow-network-id="$FLOW_NETWORK_ID" \
  --access-node-grpc-host="$ACCESS_NODE_GRPC_HOST" \
  --coinbase="$COINBASE" \
  --coa-address="$COA_ADDRESS" \
  --coa-key="$COA_KEY" \
  --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
  --bundler-enabled=true
```

### Production Setup (Cloud KMS)

```bash
# Use Cloud KMS instead of direct key
./evm-gateway run \
  --flow-network-id=flow-mainnet \
  --access-node-grpc-host=access.mainnet.nodes.onflow.org:9000 \
  --coinbase=<your-coinbase> \
  --coa-address=<your-coa-address> \
  --coa-cloud-kms-project-id=flow-evm-gateway \
  --coa-cloud-kms-location-id=global \
  --coa-cloud-kms-key-ring-id=tx-signing \
  --coa-cloud-kms-key=gw-key-1@1 \
  --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
  --bundler-enabled=true
```

## Security Checklist

- [ ] `.env` file has permissions `600` (owner read/write only)
- [ ] `.env` is in `.gitignore`
- [ ] No keys in command history (use environment variables)
- [ ] No keys in process lists (avoid `--coa-key` flag directly)
- [ ] For production: Use Cloud KMS instead of file-based keys
- [ ] Regular key rotation (if using file-based keys)
- [ ] Monitor for unauthorized access
- [ ] Use separate keys for testnet and mainnet

## Troubleshooting

### Permission Denied

```bash
# If you get permission errors, check file permissions
chmod 600 .env
chmod 600 .coa-key
```

### Environment Variable Not Found

```bash
# Make sure you've sourced the .env file
source .env

# Or export variables explicitly
export COA_KEY="your-key"
```

### Key Format Error

- COA key must be 64-character hexadecimal string
- COA address must be 16-character hexadecimal string
- No `0x` prefix needed

## Additional Resources

- [Flow EVM Gateway Setup](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup)
- [Google Cloud KMS](https://cloud.google.com/kms)
- [AWS KMS](https://aws.amazon.com/kms/)

