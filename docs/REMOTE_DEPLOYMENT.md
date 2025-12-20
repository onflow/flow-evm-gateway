# Remote Deployment Guide

This guide covers deploying the EVM Gateway remotely on a server or cloud instance.

**For AWS-specific deployment, see [AWS Deployment Guide](./AWS_DEPLOYMENT.md).**

## Deployment Options

### Option 1: Docker with systemd (Recommended for Linux Servers)

This is the recommended approach for production deployments on Linux servers.

#### Prerequisites

- Linux server (Ubuntu/Debian recommended)
- Docker installed
- systemd available
- Root or sudo access

#### Step 1: Prepare Configuration

Create configuration directory:

```bash
sudo mkdir -p /etc/flow/conf.d
sudo chmod 755 /etc/flow
```

Create environment file:

```bash
sudo nano /etc/flow/runtime-conf.env
```

Add your configuration:

```bash
# Version of the gateway to run
VERSION=latest  # or specific version tag

# Network Configuration
ACCESS_NODE_GRPC_HOST=access.testnet.nodes.onflow.org:9000
ACCESS_NODE_SPORK_HOSTS=access-001.testnet15.nodes.onflow.org:9000,access-001.testnet16.nodes.onflow.org:9000
FLOW_NETWORK_ID=flow-testnet
INIT_CADENCE_HEIGHT=211176670  # For testnet, see config/config.go

# Account Configuration
COINBASE=your-evm-coinbase-address
COA_ADDRESS=your-16-character-hex-coa-address
COA_KEY=your-64-character-hex-private-key

# ERC-4337 Configuration (if enabled)
ENTRY_POINT_ADDRESS=0xcf1e8398747a05a997e8c964e957e47209bdff08
BUNDLER_ENABLED=true
BUNDLER_BENEFICIARY=your-bundler-fee-recipient
```

**⚠️ Security**: Restrict file permissions:

```bash
sudo chmod 600 /etc/flow/runtime-conf.env
```

#### Step 2: Install systemd Service

Copy the systemd service file:

```bash
sudo cp deploy/systemd-docker/flow-evm-gateway.service /etc/systemd/system/
```

Edit the service file if needed (add ERC-4337 flags):

```bash
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

Add ERC-4337 configuration to the `ExecStart` command:

```ini
ExecStart=docker run --rm \
	--name flow-evm-gateway \
	-v /data/evm-gateway:/data \
	us-west1-docker.pkg.dev/dl-flow-devex-production/development/flow-evm-gateway:${VERSION} \
    --database-dir=/data \
    --access-node-grpc-host=${ACCESS_NODE_GRPC_HOST} \
    --flow-network-id=${FLOW_NETWORK_ID} \
    --init-cadence-height=${INIT_CADENCE_HEIGHT} \
    --coinbase=${COINBASE} \
    --coa-address=${COA_ADDRESS} \
    --coa-key=${COA_KEY} \
    --access-node-spork-hosts=${ACCESS_NODE_SPORK_HOSTS} \
    --entry-point-address=${ENTRY_POINT_ADDRESS} \
    --bundler-enabled=${BUNDLER_ENABLED} \
    --bundler-beneficiary=${BUNDLER_BENEFICIARY} \
    --bundler-interval=800ms \
    --ws-enabled=true \
    --tx-state-validation=local-index \
    --rate-limit=9999999 \
    --rpc-host=0.0.0.0 \
    --rpc-port=8545 \
    --metrics-port=9091 \
    --log-level=info
```

**Note**: Add `-v /data/evm-gateway:/data` to persist database data.

#### Step 3: Create Data Directory

```bash
sudo mkdir -p /data/evm-gateway
sudo chown $USER:$USER /data/evm-gateway  # Or appropriate user
```

#### Step 4: Enable and Start Service

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable service (start on boot)
sudo systemctl enable flow-evm-gateway

# Start service
sudo systemctl start flow-evm-gateway

# Check status
sudo systemctl status flow-evm-gateway

# View logs
sudo journalctl -u flow-evm-gateway -f
```

#### Step 5: Configure Firewall

```bash
# Allow RPC port (8545)
sudo ufw allow 8545/tcp

# Allow metrics port (9091)
sudo ufw allow 9091/tcp

# Or use iptables
sudo iptables -A INPUT -p tcp --dport 8545 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 9091 -j ACCEPT
```

### Option 2: Docker Compose

Good for development or single-server deployments.

#### Step 1: Create docker-compose.yml

```yaml
version: "3.8"

services:
  flow-evm-gateway:
    image: us-west1-docker.pkg.dev/dl-flow-devex-production/development/flow-evm-gateway:${VERSION:-latest}
    container_name: flow-evm-gateway
    restart: unless-stopped
    ports:
      - "8545:8545" # RPC
      - "9091:9091" # Metrics
    volumes:
      - ./data:/data
    environment:
      - ACCESS_NODE_GRPC_HOST=${ACCESS_NODE_GRPC_HOST}
      - ACCESS_NODE_SPORK_HOSTS=${ACCESS_NODE_SPORK_HOSTS}
      - FLOW_NETWORK_ID=${FLOW_NETWORK_ID}
      - INIT_CADENCE_HEIGHT=${INIT_CADENCE_HEIGHT}
      - COINBASE=${COINBASE}
      - COA_ADDRESS=${COA_ADDRESS}
      - COA_KEY=${COA_KEY}
      - ENTRY_POINT_ADDRESS=${ENTRY_POINT_ADDRESS}
      - BUNDLER_ENABLED=${BUNDLER_ENABLED}
      - BUNDLER_BENEFICIARY=${BUNDLER_BENEFICIARY}
    command: >
      --database-dir=/data
      --access-node-grpc-host=${ACCESS_NODE_GRPC_HOST}
      --flow-network-id=${FLOW_NETWORK_ID}
      --init-cadence-height=${INIT_CADENCE_HEIGHT}
      --coinbase=${COINBASE}
      --coa-address=${COA_ADDRESS}
      --coa-key=${COA_KEY}
      --access-node-spork-hosts=${ACCESS_NODE_SPORK_HOSTS}
      --entry-point-address=${ENTRY_POINT_ADDRESS}
      --bundler-enabled=${BUNDLER_ENABLED}
      --bundler-beneficiary=${BUNDLER_BENEFICIARY}
      --bundler-interval=800ms
      --ws-enabled=true
      --tx-state-validation=local-index
      --rate-limit=9999999
      --rpc-host=0.0.0.0
      --rpc-port=8545
      --metrics-port=9091
      --log-level=info
```

#### Step 2: Create .env File

```bash
cat > .env << EOF
VERSION=latest
ACCESS_NODE_GRPC_HOST=access.testnet.nodes.onflow.org:9000
ACCESS_NODE_SPORK_HOSTS=access-001.testnet15.nodes.onflow.org:9000
FLOW_NETWORK_ID=flow-testnet
INIT_CADENCE_HEIGHT=211176670
COINBASE=your-evm-coinbase-address
COA_ADDRESS=your-16-character-hex-coa-address
COA_KEY=your-64-character-hex-private-key
ENTRY_POINT_ADDRESS=0xcf1e8398747a05a997e8c964e957e47209bdff08
BUNDLER_ENABLED=true
BUNDLER_BENEFICIARY=your-bundler-fee-recipient
EOF

chmod 600 .env
```

#### Step 3: Start with Docker Compose

```bash
# Start in background
docker-compose up -d

# View logs
docker-compose logs -f

# Stop
docker-compose down
```

### Option 3: Direct Binary Deployment

For servers without Docker or when you need more control.

#### Step 1: Build Binary on Server

```bash
# SSH into server
ssh user@your-server

# Install Go 1.25+
# Clone repository
git clone https://github.com/onflow/flow-evm-gateway.git
cd flow-evm-gateway
git checkout $(curl -s https://api.github.com/repos/onflow/flow-evm-gateway/releases/latest | jq -r .tag_name)

# Build
CGO_ENABLED=1 go build -o evm-gateway cmd/main/main.go
chmod +x evm-gateway
```

#### Step 2: Create systemd Service (Binary)

Create `/etc/systemd/system/flow-evm-gateway.service`:

```ini
[Unit]
Description=Flow EVM Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=evm-gateway
Group=evm-gateway
WorkingDirectory=/opt/evm-gateway
ExecStart=/opt/evm-gateway/evm-gateway run \
  --database-dir=/opt/evm-gateway/data \
  --access-node-grpc-host=access.testnet.nodes.onflow.org:9000 \
  --flow-network-id=flow-testnet \
  --coinbase=your-evm-coinbase-address \
  --coa-address=your-16-character-hex-coa-address \
  --coa-key=your-64-character-hex-private-key \
  --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
  --bundler-enabled=true \
  --bundler-beneficiary=your-bundler-fee-recipient \
  --bundler-interval=800ms \
  --ws-enabled=true \
  --rpc-host=0.0.0.0 \
  --rpc-port=8545 \
  --metrics-port=9091 \
  --log-level=info
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

**Better**: Use environment file for secrets:

```ini
[Service]
EnvironmentFile=/etc/flow/runtime-conf.env
ExecStart=/opt/evm-gateway/evm-gateway run \
  --database-dir=/opt/evm-gateway/data \
  --access-node-grpc-host=${ACCESS_NODE_GRPC_HOST} \
  --flow-network-id=${FLOW_NETWORK_ID} \
  --coinbase=${COINBASE} \
  --coa-address=${COA_ADDRESS} \
  --coa-key=${COA_KEY} \
  --entry-point-address=${ENTRY_POINT_ADDRESS} \
  --bundler-enabled=${BUNDLER_ENABLED} \
  --bundler-beneficiary=${BUNDLER_BENEFICIARY} \
  --bundler-interval=800ms \
  --ws-enabled=true \
  --rpc-host=0.0.0.0 \
  --rpc-port=8545 \
  --metrics-port=9091 \
  --log-level=info
```

#### Step 3: Setup User and Permissions

```bash
# Create dedicated user
sudo useradd -r -s /bin/false evm-gateway

# Create directories
sudo mkdir -p /opt/evm-gateway/data
sudo chown evm-gateway:evm-gateway /opt/evm-gateway -R

# Copy binary
sudo cp evm-gateway /opt/evm-gateway/
sudo chown evm-gateway:evm-gateway /opt/evm-gateway/evm-gateway
```

#### Step 4: Enable and Start

```bash
sudo systemctl daemon-reload
sudo systemctl enable flow-evm-gateway
sudo systemctl start flow-evm-gateway
sudo systemctl status flow-evm-gateway
```

### Option 4: Cloud KMS for Production

For production, use Cloud KMS instead of storing keys directly.

#### Google Cloud KMS

```bash
# Install gcloud CLI and authenticate
gcloud auth login
gcloud config set project your-project-id

# Create key ring and key
gcloud kms keyrings create tx-signing --location=global
gcloud kms keys create gw-key-1 --keyring=tx-signing --location=global --purpose=asymmetric-signing --default-algorithm=ec-sign-p256-sha256

# Grant service account access
gcloud kms keys add-iam-policy-binding gw-key-1 \
  --keyring=tx-signing \
  --location=global \
  --member=serviceAccount:your-service-account@your-project.iam.gserviceaccount.com \
  --role=roles/cloudkms.signer
```

Update service to use KMS:

```ini
ExecStart=docker run --rm \
  --name flow-evm-gateway \
  -v /data/evm-gateway:/data \
  -v /path/to/gcloud-credentials.json:/gcloud-credentials.json:ro \
  -e GOOGLE_APPLICATION_CREDENTIALS=/gcloud-credentials.json \
  us-west1-docker.pkg.dev/dl-flow-devex-production/development/flow-evm-gateway:${VERSION} \
    --coa-cloud-kms-project-id=your-project-id \
    --coa-cloud-kms-location-id=global \
    --coa-cloud-kms-key-ring-id=tx-signing \
    --coa-cloud-kms-key=gw-key-1@1 \
    # ... other flags
```

## Deployment Checklist

- [ ] Server/VM provisioned with sufficient resources
- [ ] Docker installed (if using Docker deployment)
- [ ] Configuration file created with correct values
- [ ] Configuration file permissions set to 600
- [ ] Data directory created with proper permissions
- [ ] Firewall rules configured (ports 8545, 9091)
- [ ] systemd service installed and enabled
- [ ] Service started and running
- [ ] Logs checked for errors
- [ ] Health check endpoint tested
- [ ] Monitoring configured (Prometheus metrics)

## Post-Deployment Verification

### Check Service Status

```bash
# systemd
sudo systemctl status flow-evm-gateway

# Docker
docker ps | grep flow-evm-gateway
docker logs flow-evm-gateway
```

### Test RPC Endpoint

```bash
curl -X POST http://your-server:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

### Check Metrics

```bash
curl http://your-server:9091/metrics | grep evm_gateway
```

### Monitor Logs

```bash
# systemd
sudo journalctl -u flow-evm-gateway -f

# Docker
docker logs -f flow-evm-gateway
```

## Updating the Gateway

### Docker Deployment

```bash
# Update version in /etc/flow/runtime-conf.env
sudo nano /etc/flow/runtime-conf.env
# Change: VERSION=latest  to  VERSION=v1.2.3

# Restart service
sudo systemctl restart flow-evm-gateway
```

### Binary Deployment

```bash
# Stop service
sudo systemctl stop flow-evm-gateway

# Build new binary
cd /path/to/flow-evm-gateway
git pull
CGO_ENABLED=1 go build -o evm-gateway cmd/main/main.go

# Replace binary
sudo cp evm-gateway /opt/evm-gateway/
sudo systemctl start flow-evm-gateway
```

## Troubleshooting

### Service Won't Start

```bash
# Check logs
sudo journalctl -u flow-evm-gateway -n 50

# Check configuration
sudo cat /etc/flow/runtime-conf.env

# Verify Docker is running
sudo systemctl status docker
```

### Database Issues

```bash
# Check disk space
df -h /data/evm-gateway

# Check permissions
ls -la /data/evm-gateway
```

### Network Issues

```bash
# Test Access Node connectivity
telnet access.testnet.nodes.onflow.org 9000

# Check firewall
sudo ufw status
```

## Security Best Practices

1. **Use Cloud KMS for production** - Never store keys in files for production
2. **Restrict file permissions** - `chmod 600` for config files
3. **Use dedicated user** - Run service as non-root user
4. **Firewall rules** - Only expose necessary ports
5. **Regular updates** - Keep gateway and system updated
6. **Monitor access** - Set up logging and monitoring
7. **Backup configuration** - Keep secure backups of config (without keys)

## Additional Resources

- [Flow EVM Gateway Setup](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup)
- [Key Management Guide](./KEY_MANAGEMENT.md)
- [Deployment and Testing Guide](./DEPLOYMENT_AND_TESTING.md)
