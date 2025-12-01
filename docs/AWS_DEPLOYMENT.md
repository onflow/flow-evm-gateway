# AWS Deployment Guide - Custom EVM Gateway with ERC-4337

Complete step-by-step guide for deploying your custom EVM Gateway (with ERC-4337 support) to AWS and running it on Flow Testnet.

## Migrating Existing Deployment to Use Public Snapshot

**If you already have a deployment running or partially set up**, follow these steps to switch to the public snapshot method:

1. **SSH to your EC2 instance:**

   ```bash
   ssh -i ~/Downloads/your-key.pem ec2-user@your-instance-ip
   ```

2. **Stop the gateway service (if running):**

   ```bash
   sudo systemctl stop flow-evm-gateway
   ```

3. **Backup existing database (if it exists and you want to keep it):**

   ```bash
   # Check if database exists
   ls -la /data/evm-gateway/

   # If it exists and you want to backup:
   sudo mv /data/evm-gateway /data/evm-gateway-backup-$(date +%Y%m%d)
   ```

4. **Remove existing database (if you want to start fresh with snapshot):**

   ```bash
   # Remove existing database to start fresh with snapshot
   sudo rm -rf /data/evm-gateway/*
   # Or remove the directory entirely:
   sudo rm -rf /data/evm-gateway
   ```

5. **Follow Step 11 below** to install gsutil, download, and extract the snapshot.

6. **Update configuration in Step 13:**

   - Ensure `ACCESS_NODE_SPORK_HOSTS` includes both devnet51 and devnet52
   - Comment out or remove `FORCE_START_HEIGHT` (snapshot will auto-detect height)

7. **Restart the gateway:**
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl start flow-evm-gateway
   sudo systemctl status flow-evm-gateway
   ```

## Checking Your Current Status

If you've already started the deployment process, check what's been set up:

### Check What's Already Done

```bash
# Check if ECR repository exists
aws ecr describe-repositories --repository-names flow-evm-gateway --region us-east-1

# Check if Docker image was pushed (replace with your region and account ID)
export AWS_REGION=us-east-1
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
aws ecr describe-images --repository-name flow-evm-gateway --region $AWS_REGION

# Check EC2 instances
aws ec2 describe-instances --filters "Name=tag:Name,Values=flow-evm-gateway" --query 'Reservations[*].Instances[*].[InstanceId,State.Name,PublicIpAddress]' --output table

# Check Elastic IPs
aws ec2 describe-addresses --query 'Addresses[*].[PublicIp,InstanceId,AssociationId]' --output table
```

### If You Need to Start Fresh

If you want to clean up and start over:

```bash
# 1. Terminate EC2 instance (if running)
# In EC2 Console: Select instance → Instance state → Terminate instance
# Or via CLI:
aws ec2 terminate-instances --instance-ids i-xxxxxxxxxxxxx

# 2. Release Elastic IP (if allocated but not needed)
# In EC2 Console: Elastic IPs → Select IP → Actions → Release Elastic IP addresses
# Or via CLI:
aws ec2 release-address --allocation-id eipalloc-xxxxxxxxxxxxx

# 3. Delete ECR repository (optional - only if you want to remove the Docker images)
aws ecr delete-repository --repository-name flow-evm-gateway --region us-east-1 --force

# 4. Delete security group (if you created a custom one)
aws ec2 delete-security-group --group-id sg-xxxxxxxxxxxxx
```

**Note**: You typically DON'T need to reset everything. You can continue from where you left off. Only reset if:

- Your EC2 instance is in a bad state
- You want to use different instance settings
- You made configuration mistakes

### Continue From Where You Left Off

If you've completed Steps 1-6, you can continue with Step 7. Just verify:

1. **EC2 instance is running**: Check in EC2 Console
2. **Security group is configured**: Verify inbound rules in Step 5
3. **You have your values saved**: AWS_ACCOUNT_ID, AWS_REGION, VERSION from Step 3

Then proceed with Step 7 (Configure IAM Role) and continue from there.

## Prerequisites

- AWS account with appropriate permissions
- AWS CLI installed and configured on your local machine
- Docker installed on your local machine
- Your custom gateway code in this repository, tested and ready to deploy

## Part 1: Build and Push Your Custom Docker Image

### Step 1: Build Docker Image Locally

On your local machine, in the repository directory:

```bash
# Navigate to your gateway directory
cd /Users/briandoyle/src/evm-gateway

# Build the Docker image for Linux/AMD64 (required for EC2)
# Replace 'testnet-v1' with your desired tag (e.g., commit hash or branch name)

# For Apple Silicon Macs (M1/M2/M3), use buildx for cross-platform builds:
docker buildx build --platform linux/amd64 \
  --build-arg VERSION="testnet-v1" \
  --build-arg ARCH=amd64 \
  -f Dockerfile \
  -t flow-evm-gateway:testnet-v1 \
  --load .

# For Intel Macs or Linux, you can use:
# make docker-build VERSION=testnet-v1 IMAGE_TAG=testnet-v1 GOARCH=amd64
# Or:
# docker build \
#   --build-arg VERSION="testnet-v1" \
#   --build-arg ARCH=amd64 \
#   -f Dockerfile \
#   -t flow-evm-gateway:testnet-v1 .
```

**Note**: This builds the Docker image but does NOT run it. The image will be pushed to AWS ECR and run on your EC2 instance.

### Step 2: Create AWS ECR Repository

**Do this on your local machine** (in your terminal, not on the EC2 instance).

You can do this either via AWS CLI (recommended) or AWS Console:

**Option A: Using AWS CLI (Recommended)**

Run these commands in your local terminal:

```bash
# Set your AWS region - MUST match the region where your EC2 instance is located
# Common regions: us-east-1 (N. Virginia), us-east-2 (Ohio), us-west-1 (N. California), us-west-2 (Oregon)
export AWS_REGION=us-east-2  # Change this to match your EC2 instance region

# Get your AWS account ID
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

# Create ECR repository
aws ecr create-repository \
  --repository-name flow-evm-gateway \
  --region $AWS_REGION \
  --image-scanning-configuration scanOnPush=true

# Authenticate Docker with ECR
aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com
```

**Option B: Using AWS Console**

1. Go to: https://console.aws.amazon.com/ecr/
2. **Important**: Make sure you're in the same region as your EC2 instance (e.g., `us-east-2` for Ohio). Check the region selector in the top-right corner of the AWS Console.
3. Click **"Create repository"**
4. Choose **"Private"**
5. Repository name: `flow-evm-gateway`
6. Enable **"Scan on push"** (optional but recommended)
7. Click **"Create repository"**
8. After creation, click on the repository name
9. Click **"View push commands"** to see the authentication command
10. Run the authentication command shown (it will look like the one in Option A above)

**Note**: After creating the repository, you still need to authenticate Docker using the command from Option A (or the one shown in the Console).

### Step 3: Tag and Push Image to ECR

```bash
# Tag your image for ECR (use the same tag from Step 1)
docker tag flow-evm-gateway:testnet-v1 \
  $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/flow-evm-gateway:testnet-v1

# Push to ECR
docker push $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/flow-evm-gateway:testnet-v1
```

**Important**: Save these values for later (you'll need them when configuring the EC2 instance):

- `AWS_ACCOUNT_ID`: Your AWS account ID
- `AWS_REGION`: Your AWS region - **must match your EC2 instance region** (e.g., `us-east-2` for Ohio)
- `VERSION`: Your image tag (e.g., `testnet-v1`)

AWS_ACCOUNT_ID=000338030955
AWS_REGION=us-east-2
VERSION=testnet-v1

## Part 2: Setup AWS EC2 Instance

### Step 4: Launch EC2 Instance

1. Go to EC2 Console: https://console.aws.amazon.com/ec2/
2. Click "Launch Instance"
3. Configure:
   - **Name**: `flow-evm-gateway`
   - **AMI**: Amazon Linux 2023 or Ubuntu 22.04 LTS
   - **Instance Type**: `t3.small` (2 vCPU, 2 GB RAM) for demos, or `t3.large` for production
   - **Key Pair**: Select or create a new key pair for SSH access (download the .pem file)
   - **Network Settings**:
     - Create or select a VPC
     - Auto-assign Public IP: Enable
     - Security Group: Create new (we'll configure next)
4. **Storage**: 20 GB gp3 (for demos) or 30+ GB gp3 (for production)
5. Click "Launch Instance"

### Step 5: Configure Security Group

1. Go to **Security Groups** in EC2 Console
2. Find the security group created with your instance
3. Click **Edit inbound rules**
4. Add these rules:

| Type       | Protocol | Port Range | Source              | Description        |
| ---------- | -------- | ---------- | ------------------- | ------------------ |
| SSH        | TCP      | 22         | Your IP / 0.0.0.0/0 | SSH access         |
| Custom TCP | TCP      | 8545       | 0.0.0.0/0           | JSON-RPC endpoint  |
| Custom TCP | TCP      | 9091       | Your IP / VPC CIDR  | Prometheus metrics |

5. Click **Save rules**

### Step 6: Allocate Elastic IP (Optional but Recommended)

1. Go to **Elastic IPs** in EC2 Console
2. Click **Allocate Elastic IP address**
3. Select your instance and click **Associate Elastic IP address**

This gives you a static IP that won't change if you restart the instance.

### Step 7: Configure IAM Role for ECR Access

Your EC2 instance needs permission to pull images from ECR:

1. Go to **EC2 Console** → Select your instance → **Security** tab → Click on **IAM role**
2. If no role exists, click **Create IAM role**:
   - Trusted entity: EC2
   - Permissions: Search for and attach `AmazonEC2ContainerRegistryReadOnly`
   - Name: `ec2-ecr-access`
   - Click **Create role**
3. If a role exists, click **Add permissions** → **Attach policies** → Search for `AmazonEC2ContainerRegistryReadOnly` → Attach

## Part 3: Setup EC2 Instance

### Step 8: Connect to EC2 Instance

On your local machine:

```bash
# Set permissions on your .pem file (usually in ~/Downloads)
chmod 400 ~/Downloads/your-key.pem

# Find your instance's Public IPv4 DNS or IP from EC2 Console
# For Amazon Linux 2023:
ssh -i ~/Downloads/your-key.pem ec2-user@your-instance-public-ip

# For Ubuntu:
ssh -i ~/Downloads/your-key.pem ubuntu@your-instance-public-ip
```

### Step 9: Install Docker on EC2

**For Amazon Linux 2023:**

```bash
sudo dnf install -y docker
sudo systemctl start docker
sudo systemctl enable docker
sudo usermod -aG docker ec2-user
exit
# SSH back in
```

**For Ubuntu:**

```bash
sudo apt install -y docker.io
sudo systemctl start docker
sudo systemctl enable docker
sudo usermod -aG docker ubuntu
exit
# SSH back in
```

Verify Docker:

```bash
docker --version
docker ps
```

### Step 10: Install AWS CLI on EC2

**For Amazon Linux 2023:**

```bash
sudo dnf install -y aws-cli
```

**For Ubuntu:**

```bash
sudo apt install -y awscli
```

### Step 11: Setup Data Directory Using Public Snapshot

**Use Public Database Snapshot (Required - Saves Days of Syncing)**

Flow provides public database snapshots hosted on Google Cloud Storage that you can use to bootstrap your EVM Gateway without syncing from genesis.

**Public Snapshot Buckets:**

- **Testnet/Devnet**: `gs://evm-gw-db-devnet-public`
- **Mainnet**: `gs://evm-gw-db-mainnet-public`

Browse available snapshots: [Testnet Bucket](https://console.cloud.google.com/storage/browser/evm-gw-db-devnet-public) | [Mainnet Bucket](https://console.cloud.google.com/storage/browser/evm-gw-db-mainnet-public)

**Steps to Use a Snapshot:**

1. **Install Google Cloud SDK (gsutil) on EC2:**

   **For Amazon Linux 2023:**

   ```bash
   # Install Python 3 and pip if not already installed
   sudo dnf install -y python3 python3-pip python3-devel gcc

   # Install crcmod for faster large file downloads (recommended)
   pip3 install crcmod

   # Install gsutil
   pip3 install gsutil

   # Add to PATH (add to ~/.bashrc for persistence)
   echo 'export PATH=$PATH:~/.local/bin' >> ~/.bashrc
   source ~/.bashrc
   ```

   **For Ubuntu:**

   ```bash
   # Install gsutil and dependencies
   sudo apt install -y gsutil python3-crcmod
   ```

   **Note:** Installing `crcmod` enables faster downloads for large files (like the database snapshot). The download will work without it, but will be slower.

2. **Create data directory and set permissions:**

   ```bash
   # Create the directory
   sudo mkdir -p /data

   # Make sure you have write permissions (important for download)
   sudo chown ec2-user:ec2-user /data
   sudo chmod 755 /data
   ```

3. **Download the snapshot from GCS:**

   **For Testnet:**

   ```bash
   cd /data
   # Download the snapshot (use the exact date folder - e.g., nov-25-2025)
   gsutil -m cp -r gs://evm-gw-db-devnet-public/nov-25-2025/db.tar .
   ```

   **Note:** If you get a permission error, make sure you're in a directory you own:

   ```bash
   # If still having issues, try downloading to your home directory first
   cd ~
   gsutil -m cp -r gs://evm-gw-db-devnet-public/nov-25-2025/db.tar .
   # Then move it to /data
   sudo mv db.tar /data/
   sudo chown ec2-user:ec2-user /data/db.tar
   ```

   **For Mainnet:**

   ```bash
   cd /data
   # First, list what's available in the bucket
   gsutil ls gs://evm-gw-db-mainnet-public/

   # Then list contents of a specific folder (replace with actual folder name from above)
   gsutil ls gs://evm-gw-db-mainnet-public/nov-24-2025/

   # Download the snapshot (use the exact path from the listing above)
   gsutil -m cp -r gs://evm-gw-db-mainnet-public/nov-24-2025/db.tar .
   ```

4. **Extract the snapshot:**

   ```bash
   cd /data
   # Extract the database (this will create the 'db' folder)
   tar -xvf db.tar
   ```

   **Important:** The snapshot extracts to a `db` folder, but our gateway expects `/data/evm-gateway`. Rename it:

   ```bash
   # Rename 'db' to 'evm-gateway' to match our configuration
   if [ -d "db" ]; then
       mv db evm-gateway
   fi

   # Set ownership
   sudo chown -R ec2-user:ec2-user /data/evm-gateway

   # Clean up the tar file to save space
   rm db.tar
   ```

   **Note:** The original instructions use `/etc/config/db`, but our AWS setup uses `/data/evm-gateway` to match the Docker volume mount in the systemd service file.

5. **Update FORCE_START_HEIGHT in Step 13:**

   When you configure the gateway in Step 13, you may need to adjust `FORCE_START_HEIGHT` based on the snapshot.
   The snapshot contains the complete EVM state at a specific height, so you don't need to start from genesis.
   The gateway will automatically detect the existing database and continue syncing from the last block in the snapshot.

   **Note:** Check the snapshot folder name or metadata to determine the snapshot's height. If unsure, you can start the gateway and check the logs to see what height it detects.

**Important Notes:**

- **Snapshot size**: Expect 50-100GB+ for a fully synced database - ensure you have enough disk space
- **Download time**: The snapshot download may take 30-60 minutes depending on your connection speed
- **Network**: Use the testnet bucket (`evm-gw-db-devnet-public`) for testnet deployments
- **With a snapshot, you can start from the snapshot's height, not genesis** - the snapshot contains the EVM state at that height
- The gateway will automatically detect the existing database and continue syncing from the last block

**Alternative: Fresh Start from Genesis (Not Recommended - Only if Snapshot Fails)**

If the snapshot download fails or you need to sync from scratch for a specific reason:

```bash
# Create data directory
sudo mkdir -p /data/evm-gateway

# Set ownership (replace 'ec2-user' with 'ubuntu' if using Ubuntu)
sudo chown ec2-user:ec2-user /data/evm-gateway
```

**Note:** Syncing from genesis will take 1-3 days depending on CPU. Using the public snapshot (Option B above) is strongly recommended.

### Step 11.5: Expand Storage if Needed

If your EVM state database grows larger than your initial storage allocation (e.g., 57GB+), you'll need to expand your EBS volume.

**Check current storage usage:**

```bash
# On EC2 instance
df -h
```

**Expand EBS Volume:**

1. **In AWS Console:**

   - Go to **EC2 Console** → **Volumes**
   - Select your volume (e.g., `vol-0f8729596da45963b`)
   - Click **Actions** → **Modify Volume**
   - Set new size (e.g., 80GB or 100GB for safety)
   - Click **Modify**

2. **Wait for modification to complete** (check volume state in console)

3. **Resize filesystem on EC2 instance:**

```bash
# Check current filesystem size
df -h

# Install growpart if needed (for resizing partitions)
# Amazon Linux 2023:
sudo dnf install -y cloud-utils-growpart
# Ubuntu:
sudo apt install -y cloud-guest-utils

# First, resize the partition to use the full volume
sudo growpart /dev/nvme0n1 1
# (Replace '1' with your partition number if different)

# Then resize the filesystem
# For XFS filesystem (Amazon Linux 2023):
sudo xfs_growfs /

# For ext4 filesystem (Ubuntu):
sudo resize2fs /dev/nvme0n1p1
# Or if using older instance types:
sudo resize2fs /dev/xvda1

# Verify new size
df -h
```

**Note:** The `growpart` step is required to resize the partition before resizing the filesystem. Without it, `xfs_growfs` or `resize2fs` won't see the additional space.

**Note:** The EVM state database can grow significantly. For production, allocate at least 100GB initially, or monitor and expand as needed.

### Step 12: Create ECR Authentication Script

```bash
# Create the script
sudo nano /usr/local/bin/ecr-login.sh
```

Paste this content (replace `us-east-1` with your AWS region):

```bash
#!/bin/bash
AWS_REGION=us-east-2
aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin \
  $(aws sts get-caller-identity --query Account --output text).dkr.ecr.$AWS_REGION.amazonaws.com
```

Save and make executable:

```bash
sudo chmod +x /usr/local/bin/ecr-login.sh
```

### Step 13: Create Configuration File

```bash
# Create config directory
sudo mkdir -p /etc/flow/conf.d
sudo chmod 755 /etc/flow

# Create environment file
sudo nano /etc/flow/runtime-conf.env
```

Paste this configuration (replace all placeholder values with your actual values):

```bash
# Docker Image Configuration
AWS_ACCOUNT_ID=000338030955
AWS_REGION=us-east-2
VERSION=testnet-v1

# Network Configuration
ACCESS_NODE_GRPC_HOST=access.devnet.nodes.onflow.org:9000
FLOW_NETWORK_ID=flow-testnet

# Spork Hosts Configuration - Option 2: Original EVM Genesis
# Starting from original EVM genesis (211176670) requires both devnet51 and devnet52 spork hosts
# to access historical blocks from the original genesis through the current spork.
ACCESS_NODE_SPORK_HOSTS=access-001.devnet51.nodes.onflow.org:9000,access-001.devnet52.nodes.onflow.org:9000

# IMPORTANT: For fresh database initialization, you MUST start from an EVM genesis height.
# The gateway initializes EVM state from block 0, so starting from an arbitrary height will
# cause "nonce too high" errors because transactions expect the EVM state that existed at that height.
#
# Genesis Heights:
# - Original EVM genesis (pre-Forte): 211176670 - requires devnet51 + devnet52 spork hosts (CURRENTLY CONFIGURED)
# - Post-Forte genesis (current): 218215349 - only requires devnet52 spork host
#
# FORCE_START_HEIGHT: When using a public snapshot (recommended), the gateway will automatically
# detect the height from the snapshot database. You typically don't need to set FORCE_START_HEIGHT
# when using a snapshot - the gateway reads it from the database.
#
# Only set this if:
# - You're doing a fresh start from genesis (not recommended - takes 1-3 days)
# - You need to override the snapshot's stored height for a specific reason
#
# If not set and using a snapshot, the gateway will use the height stored in the snapshot database.
# If not set and no database exists, the gateway will use the hardcoded value (218215349).
#
# For snapshot users: Leave this commented out or remove it - the gateway will auto-detect the height.
# FORCE_START_HEIGHT=211176670

# Account Configuration
COINBASE=your-evm-coinbase-address
COA_ADDRESS=your-16-character-hex-coa-address
COA_KEY=your-64-character-hex-private-key

# ERC-4337 Configuration (Required)
ENTRY_POINT_ADDRESS=0xcf1e8398747a05a997e8c964e957e47209bdff08
BUNDLER_ENABLED=true
BUNDLER_BENEFICIARY=your-bundler-fee-recipient-address

# ERC-4337 Configuration (Optional)
BUNDLER_INTERVAL=800ms
MAX_OPS_PER_BUNDLE=10
USER_OP_TTL=5m
```

**Replace these values:**

- `AWS_ACCOUNT_ID`: From Step 3
- `AWS_REGION`: From Step 3
- `VERSION`: From Step 1 (e.g., `testnet-v1`)
- `COINBASE`: Your EVM coinbase address (remove 0x prefix)
- `COA_ADDRESS`: Your 16-character hex COA address (remove 0x prefix)
- `COA_KEY`: Your 64-character hex private key (remove 0x prefix)
- `BUNDLER_BENEFICIARY`: Your bundler fee recipient address (remove 0x prefix)

Save and restrict permissions:

```bash
sudo chmod 600 /etc/flow/runtime-conf.env
```

### Step 14: Create systemd Service File

```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

Paste this complete service file:

```ini
[Unit]
Description=EVM Gateway running with Docker (Custom Build)
Requires=docker.service
After=network-online.target docker.service

[Install]
Alias=evm-gateway.service
WantedBy=default.target

[Service]
Type=simple
TimeoutStopSec=1m
RestartSec=5s
Restart=always
StandardOutput=journal

EnvironmentFile=/etc/flow/runtime-conf.env
EnvironmentFile=-/etc/flow/conf.d/*.env

# Authenticate with ECR before pulling
ExecStartPre=/usr/local/bin/ecr-login.sh
# Pull custom Docker image from ECR
ExecStartPre=/usr/bin/docker pull ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
ExecStart=/usr/bin/docker run --rm \
	--name flow-evm-gateway \
	-p 8545:8545 \
	-v /data/evm-gateway:/data \
	${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION} \
    --database-dir=/data \
    --access-node-grpc-host=${ACCESS_NODE_GRPC_HOST} \
    --flow-network-id=${FLOW_NETWORK_ID} \
    --force-start-height=${FORCE_START_HEIGHT} \
    --coinbase=${COINBASE} \
    --coa-address=${COA_ADDRESS} \
    --coa-key=${COA_KEY} \
    --access-node-spork-hosts=${ACCESS_NODE_SPORK_HOSTS} \
    --entry-point-address=${ENTRY_POINT_ADDRESS} \
    --bundler-enabled=${BUNDLER_ENABLED} \
    --bundler-beneficiary=${BUNDLER_BENEFICIARY} \
    --bundler-interval=${BUNDLER_INTERVAL} \
    --max-ops-per-bundle=${MAX_OPS_PER_BUNDLE} \
    --user-op-ttl=${USER_OP_TTL} \
    --ws-enabled=true \
    --tx-state-validation=local-index \
    --rate-limit=9999999 \
    --rpc-host=0.0.0.0 \
    --rpc-port=8545 \
    --metrics-port=9091 \
    --log-level=info
ExecStop=/usr/bin/docker stop flow-evm-gateway
```

Save the file.

### Step 15: Enable and Start Service

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable service (start on boot)
sudo systemctl enable flow-evm-gateway

# Start service
sudo systemctl start flow-evm-gateway

# Check status
sudo systemctl status flow-evm-gateway

# View logs (follow in real-time)
sudo journalctl -u flow-evm-gateway -f
```

## Part 4: Expose Gateway for Public Access

### Step 16: Update Systemd Service to Publish Port

If you haven't already added `-p 8545:8545` to the Docker command, do it now:

```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

Find the `ExecStart` line and ensure it has `-p 8545:8545` right after `--name flow-evm-gateway`:

```ini
ExecStart=/usr/bin/docker run --rm \
	--name flow-evm-gateway \
	-p 8545:8545 \
	-v /data/evm-gateway:/data \
```

Save and reload:

```bash
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
```

### Step 17: Test Locally on EC2

First, verify the gateway responds on the host:

```bash
curl -X POST http://127.0.0.1:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

You should see a JSON response with a block number. If this fails, check the service logs:

```bash
sudo journalctl -u flow-evm-gateway -n 50 --no-pager
```

### Step 18: Open AWS Security Group

1. Go to **AWS Console → EC2 → Instances**
2. Select your EC2 instance
3. Click the **Security** tab
4. Click the security group link
5. Click **Edit inbound rules**
6. Click **Add rule**:
   - **Type**: Custom TCP
   - **Port range**: `8545`
   - **Source**: `0.0.0.0/0` (or your specific IP/CIDR for better security)
   - **Description**: "EVM Gateway JSON-RPC"
7. Click **Save rules**

### Step 19: Test from Remote Client

From your local machine (replace `YOUR_EC2_PUBLIC_IP` with your actual EC2 public IP):

```bash
curl -X POST http://YOUR_EC2_PUBLIC_IP:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

Expected response:

```json
{ "jsonrpc": "2.0", "id": 1, "result": "0x..." }
```

If you get `Connection refused` or timeout:

- Verify the security group rule was saved
- Check that the systemd service is running: `sudo systemctl status flow-evm-gateway`
- Verify port is published: `docker ps | grep flow-evm-gateway` (should show `0.0.0.0:8545->8545/tcp`)

## Part 5: Verify Deployment

### Step 20: Verify Service is Running

On the EC2 instance:

```bash
# Check service status
sudo systemctl status flow-evm-gateway

# Check Docker container
docker ps | grep flow-evm-gateway

# Check logs
sudo journalctl -u flow-evm-gateway -n 50 --no-pager
```

### Step 21: Check Metrics

```bash
curl http://your-ec2-ip:9091/metrics | grep evm_gateway | head -20
```

## Troubleshooting

### Service Won't Start

```bash
# Check Docker
sudo systemctl status docker
docker ps -a

# Check logs
sudo journalctl -u flow-evm-gateway -n 100

# Check configuration
sudo cat /etc/flow/runtime-conf.env

# Test ECR login manually
/usr/local/bin/ecr-login.sh
docker pull ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
```

### Connection Issues

```bash
# Test Access Node connectivity
telnet access.devnet.nodes.onflow.org 9000

# Check DNS resolution
nslookup access.devnet.nodes.onflow.org
nslookup access-001.devnet51.nodes.onflow.org
```

### ECR Authentication Issues

```bash
# Verify IAM role is attached
aws sts get-caller-identity

# Test ECR access
aws ecr describe-repositories --region $AWS_REGION
```

## Updating Your Deployment

When you make changes to your gateway code:

1. **Build new image** (Step 1) with a new tag (e.g., `testnet-v2`)
2. **Push to ECR** (Step 3) with the new tag
3. **Update config** on EC2:
   ```bash
   sudo nano /etc/flow/runtime-conf.env
   # Change VERSION=testnet-v2
   ```
4. **Restart service**:
   ```bash
   sudo systemctl restart flow-evm-gateway
   ```

## Maintenance

### View Logs

```bash
# Follow logs in real-time
sudo journalctl -u flow-evm-gateway -f

# View last 100 lines
sudo journalctl -u flow-evm-gateway -n 100
```

### Restart Service

```bash
sudo systemctl restart flow-evm-gateway
```

### Stop Service

```bash
sudo systemctl stop flow-evm-gateway
```

### Check Resource Usage

```bash
# Check Docker container stats
docker stats flow-evm-gateway

# Check disk space
df -h

# Check memory
free -h
```
