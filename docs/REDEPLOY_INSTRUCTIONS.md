# Exact Redeploy Instructions - Signature Recovery Fix (Recovery ID Conversion)

## Complete Step-by-Step Instructions

### On Your Local Machine

#### Step 1: Build New Docker Image

```bash
cd /Users/briandoyle/src/evm-gateway

export VERSION=no-keys-logging

docker buildx build --platform linux/amd64 \
  --build-arg VERSION="${VERSION}" \
  --build-arg ARCH=amd64 \
  -f Dockerfile \
  -t flow-evm-gateway:${VERSION} \
  --load .
```

**Wait for build to complete** (this will take several minutes)

#### Step 2: Authenticate with AWS ECR

```bash
export AWS_ACCOUNT_ID=000338030955
export AWS_REGION=us-east-2

aws ecr get-login-password --region ${AWS_REGION} | \
  docker login --username AWS --password-stdin \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com
```

**Expected**: `Login Succeeded`

#### Step 3: Tag and Push to ECR

```bash
docker tag flow-evm-gateway:${VERSION} \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}

docker push ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
```

**Wait for push to complete** - you'll see layers being pushed and a final digest

---

### On Your EC2 Instance (SSH in)

#### Step 4: SSH to EC2

```bash
ssh -i ~/Downloads/your-key.pem ec2-user@3.150.43.95
```

#### Step 5: Update VERSION and Verify Configuration

```bash
sudo nano /etc/flow/runtime-conf.env
```

Find and update the VERSION line:

```
VERSION=no-keys-logging
```

**Also verify these are set** (add if missing):

```
ENTRY_POINT_ADDRESS=0xcf1e8398747a05a997e8c964e957e47209bdff08
ENTRY_POINT_SIMULATIONS_ADDRESS=0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
BUNDLER_ENABLED=true
WALLET_API_KEY=your-64-character-hex-private-key
```

**⚠️ Important**: If `BUNDLER_ENABLED=true`, you **must** also set `WALLET_API_KEY` with the ECDSA private key that corresponds to your `COINBASE` address. The Coinbase address must be an EOA (not a smart contract) for the bundler to work. See [Deployment Guide](./DEPLOYMENT_AND_TESTING.md#-important-coinbase-and-walletkey-requirements-for-bundler) for details.

**Save**: `Ctrl+O`, `Enter`, `Ctrl+X`

#### Step 5b: Verify Service File Has Required Flags

```bash
sudo cat /etc/systemd/system/flow-evm-gateway.service | grep -E "entry-point|bundler"
```

**Expected output should include:**

```
--entry-point-address=${ENTRY_POINT_ADDRESS} \
--entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS} \
--bundler-enabled=${BUNDLER_ENABLED} \
--wallet-api-key=${WALLET_API_KEY}
```

**Note**: `--wallet-api-key` is required when bundler is enabled. It must be the ECDSA private key for your Coinbase address.

**If missing**, add them after `--log-level=error`:

```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

Add these lines (with proper backslashes):

```ini
    --log-level=error \
    --entry-point-address=${ENTRY_POINT_ADDRESS} \
    --entry-point-simulations-address=${ENTRY_POINT_SIMULATIONS_ADDRESS} \
    --bundler-enabled=${BUNDLER_ENABLED}
```

#### Step 6: Reload Systemd and Restart Service

```bash
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
sudo systemctl status flow-evm-gateway --no-pager
```

**Expected**: Service should show `active (running)`

#### Step 7: Verify New Version and Configuration

```bash
# Check version in logs
sudo journalctl -u flow-evm-gateway -n 20 --no-pager | grep -E "version|EntryPointSimulations|entryPointSimulationsAddress"

# Should show:
# - "version":"testnet-v1-entrypoint-simulations-fix"
# - "EntryPointSimulations configured - will use for simulateValidation calls"
# - "entryPointSimulationsAddress":"0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3"
```

**If you see "EntryPointSimulations not configured"**, check:

1. Environment variable is set in `/etc/flow/runtime-conf.env`
2. Service file has the `--entry-point-simulations-address` flag
3. Service was restarted after changes

**Note on RPC Visibility**: The EntryPointSimulations contract is verified on Flowscan at `0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3`. If the gateway RPC can't see it (empty bytecode queries), this is an RPC sync/indexing issue, not a deployment problem. The gateway will proceed with calls even if the RPC can't query the contract's bytecode. See `docs/RPC_SYNC_ISSUE.md` for more details.

#### Step 8: Test Gateway

```bash
# Test basic RPC
docker run --rm --network container:flow-evm-gateway curlimages/curl \
  -s -X POST http://127.0.0.1:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

**Expected**: JSON response with block number

#### Step 9: Monitor Logs (Optional - for testing)

```bash
# Watch logs filtered for UserOperation activity (excludes ingestion noise)
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion" | grep -iE "user|validation|error|api|sendUserOperation|simulation|signature|entrypoint|owner|recovered|revert"
```

**Alternative - Show only UserOp API and validation logs:**

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|sendUserOperation|simulateValidation|entrypoint|ownerFromInitCode|recoveredSigner|signerMatchesOwner|validation.*reverted|rawFactoryAddress|rawFunctionSelector|factoryAddress|functionSelector|initCodeHex"
```

---

## What Was Fixed

1. **Critical Bug Fix: Stale Nonce Issue in eth_getTransactionCount**:

   - **Problem**: The gateway was returning stale nonces when `eth_getTransactionCount` was called with `"pending"` block tag. This caused transaction failures when multiple transactions were submitted in quick succession.
   - **Root Cause**: The original gateway code treated `"pending"` the same as `"latest"` - both only returned the nonce from the latest block state, never checking the transaction pool for pending transactions.
   - **Fix**: Extended `TxPool` interface to support querying pending nonces. Updated `GetTransactionCount` to account for pending transactions when `"pending"` block tag is requested. The gateway now returns the maximum of (block state nonce, highest pending nonce + 1).
   - **Impact**: Frontends can now safely submit multiple transactions in quick succession without "nonce too low" errors. Gateway now complies with Ethereum JSON-RPC specification for `"pending"` block tag.
   - **See**: `docs/STALE_NONCE_BUG_FIX.md` for detailed documentation.

2. **Critical Fix: handleOps ABI Encoding**:

   - **Root Cause**: `EncodeHandleOps` was using `[]interface{}` with anonymous structs, which the ABI encoder cannot handle
   - **Fix**: Created named `UserOperationABI` struct type and use `[]UserOperationABI` instead
   - **Impact**: Bundler can now successfully create `handleOps` transactions and submit them to the network
   - **Symptom**: UserOps were accepted but never included because transactions couldn't be created

3. **Enhanced Bundler Logging**:

   - Bundler ticks now log at Info level with pending count
   - Logs transaction creation and submission with success/failure counts
   - Better error messages for bundler failures

4. **EntryPointSimulations Support** (from previous version):

   - Removed unreliable bytecode selector check
   - Handles RPC sync/visibility issues gracefully
   - EntryPointSimulationsAddress is required when bundler is enabled

5. **Previous Fixes** (from earlier versions):
   - Hash calculation fix: UserOp hash matches client calculation (EntryPoint v0.9.0 format)
   - Enhanced logging: Comprehensive logging for EntryPoint validation debugging
   - Signature recovery fix: Converts EIP-155 v values (27/28) to recovery IDs (0/1)
   - FailedOp and FailedOpWithRevert error decoding
   - Raw initCode logging for account creation debugging

## Quick Reference

- **AWS_ACCOUNT_ID**: `000338030955`
- **AWS_REGION**: `us-east-2`
- **VERSION**: `no-keys-logging`
- **EC2 IP**: `3.150.43.95`
- **Config File**: `/etc/flow/runtime-conf.env`

## Rollback (if needed)

```bash
sudo nano /etc/flow/runtime-conf.env
# Change VERSION back to: testnet-v1-fix
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
```
