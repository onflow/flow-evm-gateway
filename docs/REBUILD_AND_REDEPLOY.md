# Rebuild and Redeploy Instructions - Signature Validation Fix

## Quick Summary

Rebuild the Docker image with the fix, push to ECR, update the EC2 service to use the new image, and restart.

## Step-by-Step Instructions

### Step 1: Build New Docker Image (Local Machine)

On your local machine, in the repository directory:

```bash
# Navigate to your gateway directory
cd /Users/briandoyle/src/evm-gateway

# Set your version tag (use a new tag to distinguish from previous version)
export VERSION=testnet-v1-fix

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

**Note**: Replace `testnet-v1-fix` with your desired tag (e.g., `testnet-v1.1`, `testnet-v1-sigfix`, or use a commit hash).

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

### Step 3: Tag and Push Image to ECR

```bash
# Tag your image for ECR
docker tag flow-evm-gateway:${VERSION} \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}

# Push to ECR
docker push ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
```

**Expected output**: You should see the image being pushed layer by layer. Wait for "Pushed" confirmation.

### Step 4: SSH to EC2 Instance

```bash
# Replace with your actual key and IP
ssh -i ~/Downloads/your-key.pem ec2-user@3.150.43.95
```

### Step 5: Update Systemd Service File

Edit the systemd service file to use the new image version:

```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

Find the line that sets `VERSION` in the `[Service]` section's `EnvironmentFile` or directly in the `ExecStartPre` line. Update it:

**Option A: If VERSION is in `/etc/flow/runtime-conf.env`:**

```bash
sudo nano /etc/flow/runtime-conf.env
```

Change:
```bash
VERSION=testnet-v1
```

To:
```bash
VERSION=testnet-v1-fix
```

**Option B: If VERSION is hardcoded in the service file:**

Find the line:
```ini
ExecStartPre=/usr/bin/docker pull ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
```

And the ExecStart line:
```ini
ExecStart=/usr/bin/docker run --rm \
	--name flow-evm-gateway \
	-p 8545:8545 \
	-v /data/evm-gateway:/data \
	${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION} \
```

Make sure both use the same `${VERSION}` variable (it should be set in the EnvironmentFile).

**Save the file**: Ctrl+O, Enter, Ctrl+X

### Step 6: Reload Systemd and Restart Service

```bash
# Reload systemd to pick up changes
sudo systemctl daemon-reload

# Stop the current service
sudo systemctl stop flow-evm-gateway

# Start the service (it will pull the new image)
sudo systemctl start flow-evm-gateway

# Check status
sudo systemctl status flow-evm-gateway --no-pager
```

### Step 7: Verify the New Image is Running

```bash
# Check Docker container
docker ps | grep flow-evm-gateway

# Check logs to confirm it's using the new image
sudo journalctl -u flow-evm-gateway -n 50 --no-pager

# Look for the image tag in the logs - should show testnet-v1-fix
```

### Step 8: Test the Gateway

```bash
# Test from inside Docker network (since port 8545 is published)
docker run --rm --network container:flow-evm-gateway curlimages/curl \
  -s -X POST http://127.0.0.1:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

You should see a JSON response with a block number.

### Step 9: Monitor Logs for Signature Validation

Watch the logs to see the new signature validation behavior:

```bash
# Follow logs in real-time
sudo journalctl -u flow-evm-gateway -f
```

When a UserOperation for account creation is submitted, you should see:
```
skipping off-chain signature validation for account creation - EntryPoint will validate against owner
```

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
   export VERSION=testnet-v1-fix
   sudo docker pull ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
   ```

### If you want to rollback:

1. Edit `/etc/flow/runtime-conf.env` (or service file) and change `VERSION` back to `testnet-v1`
2. Run:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart flow-evm-gateway
   ```

## Quick Reference

**Your current values:**
- AWS_ACCOUNT_ID: `000338030955`
- AWS_REGION: `us-east-2`
- Old VERSION: `testnet-v1`
- New VERSION: `testnet-v1-fix` (or your choice)

**EC2 Instance:**
- IP: `3.150.43.95`
- Service: `flow-evm-gateway.service`
- Config: `/etc/flow/runtime-conf.env`

## What Changed

The fix:
- Skips off-chain signature validation for account creation (when `initCode` is present)
- Lets EntryPoint's `simulateValidation` handle signature validation correctly
- Adds enhanced logging for signature validation debugging

This means UserOperations for account creation will now pass validation and be processed correctly.

