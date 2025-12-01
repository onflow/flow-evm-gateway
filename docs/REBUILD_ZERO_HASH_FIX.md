# Rebuild and Redeploy Instructions - Zero Hash Fix

## Quick Summary

Rebuild the Docker image with enhanced logging for UserOperation debugging, push to ECR, update the EC2 service to use the new image, and restart.

## Step-by-Step Instructions

### Step 1: Build New Docker Image (Local Machine)

On your local machine, in the repository directory:

```bash
# Navigate to your gateway directory
cd /Users/briandoyle/src/evm-gateway

# Set your version tag (use a new tag to distinguish from previous version)
export VERSION=testnet-v1-zerohash-fix

# For Apple Silicon Macs (M1/M2/M3), use buildx:
docker buildx build --platform linux/amd64 \
  --build-arg VERSION="${VERSION}" \
  --build-arg ARCH=amd64 \
  -f Dockerfile \
  -t flow-evm-gateway:${VERSION} \
  --load .

# For Intel Macs or Linux:
# docker build \
#   --build-arg VERSION="${VERSION}" \
#   --build-arg ARCH=amd64 \
#   -f Dockerfile \
#   -t flow-evm-gateway:${VERSION} .
```

**Note**: This will take several minutes. Wait for the build to complete.

### Step 2: Authenticate Docker with ECR

```bash
# Set your AWS credentials (from your deployment)
export AWS_ACCOUNT_ID=000338030955
export AWS_REGION=us-east-2

# Authenticate Docker with ECR
aws ecr get-login-password --region ${AWS_REGION} | \
  docker login --username AWS --password-stdin \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com
```

**Expected output**: `Login Succeeded`

### Step 3: Tag and Push Image to ECR

```bash
# Tag your image for ECR
docker tag flow-evm-gateway:${VERSION} \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}

# Push to ECR
docker push ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
```

**Expected output**: You should see the image being pushed layer by layer. Wait for "Pushed" confirmation and the digest.

### Step 4: SSH to EC2 Instance

```bash
# Replace with your actual key path
ssh -i ~/Downloads/your-key.pem ec2-user@3.150.43.95
```

### Step 5: Update VERSION in Configuration

Edit the runtime configuration file:

```bash
sudo nano /etc/flow/runtime-conf.env
```

Find the line with `VERSION=` and change it to:

```bash
VERSION=testnet-v1-zerohash-fix
```

**Save the file**: 
- Press `Ctrl+O` (write out)
- Press `Enter` (confirm filename)
- Press `Ctrl+X` (exit)

### Step 6: Reload Systemd and Restart Service

```bash
# Reload systemd to pick up changes
sudo systemctl daemon-reload

# Restart the service (it will pull the new image)
sudo systemctl restart flow-evm-gateway

# Check status
sudo systemctl status flow-evm-gateway --no-pager
```

**Expected output**: Service should show as `active (running)`

### Step 7: Verify the New Image is Running

```bash
# Check Docker container
docker ps | grep flow-evm-gateway

# Check logs to confirm it's using the new image
sudo journalctl -u flow-evm-gateway -n 20 --no-pager | grep version

# Look for the version tag in the logs - should show testnet-v1-zerohash-fix
```

### Step 8: Test the Gateway

```bash
# Test basic RPC functionality
docker run --rm --network container:flow-evm-gateway curlimages/curl \
  -s -X POST http://127.0.0.1:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

You should see a JSON response with a block number.

### Step 9: Monitor Logs for UserOperation Requests

Watch the logs in real-time to see UserOperation requests:

```bash
# Follow logs in real-time
sudo journalctl -u flow-evm-gateway -f
```

When a UserOperation is submitted, you should now see:
- `"received eth_sendUserOperation request"` - when the request arrives
- `"user operation validation failed"` - if validation fails (with detailed error)
- `"user operation submitted"` - if successful

### Step 10: Test UserOperation Submission

From your frontend or using curl, submit a UserOperation and watch the logs. You should see detailed logging about:
- When the request is received
- Validation steps
- Any errors with full context

## Troubleshooting

### If the service fails to start:

1. **Check logs**:
   ```bash
   sudo journalctl -u flow-evm-gateway -n 100 --no-pager
   ```

2. **Check if image was pulled**:
   ```bash
   docker images | grep flow-evm-gateway
   ```

3. **Test ECR authentication**:
   ```bash
   sudo /usr/local/bin/ecr-login.sh
   ```

4. **Manually pull the image**:
   ```bash
   export AWS_ACCOUNT_ID=000338030955
   export AWS_REGION=us-east-2
   export VERSION=testnet-v1-zerohash-fix
   sudo docker pull ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
   ```

### If you want to rollback:

1. Edit `/etc/flow/runtime-conf.env` and change `VERSION` back to `testnet-v1-fix`
2. Run:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart flow-evm-gateway
   ```

## Quick Reference

**Your current values:**
- AWS_ACCOUNT_ID: `000338030955`
- AWS_REGION: `us-east-2`
- Old VERSION: `testnet-v1-fix`
- New VERSION: `testnet-v1-zerohash-fix`

**EC2 Instance:**
- IP: `3.150.43.95`
- Service: `flow-evm-gateway.service`
- Config: `/etc/flow/runtime-conf.env`

## What Changed

The fix includes:
1. **Block Height Fix (CRITICAL)**: Validator now uses indexed height instead of network height - fixes "entity not found" error
2. **Request logging**: Logs when `eth_sendUserOperation` requests are received
3. **Enhanced validation logging**: Detailed error messages with EntryPoint address, block height, and validation context
4. **Safety check**: Prevents zero hash from being returned as valid result
5. **Blocks storage access**: Validator now has access to blocks storage to query indexed height

**Root Cause Fixed**: The validator was using the network's latest height (which may not be indexed yet), causing "entity not found" errors. Now it uses the latest indexed height, ensuring all queries use blocks that exist in the local database.

## Next Steps After Deployment

1. Submit a UserOperation from your frontend
2. Watch the logs in real-time: `sudo journalctl -u flow-evm-gateway -f`
3. Look for:
   - `"received eth_sendUserOperation request"` - confirms request reached gateway
   - `"user operation validation failed"` - shows the actual validation error
   - Any other error messages

Share the log output to identify the root cause of the zero hash issue.

