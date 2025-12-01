# ERC-4337 Deployment and Testing Guide

This guide walks you through deploying and testing the EVM Gateway with ERC-4337 (Account Abstraction) support on Flow Testnet.

**For remote server deployment:**

- **AWS**: See [AWS Deployment Guide](./AWS_DEPLOYMENT.md) for step-by-step AWS instructions
- **Other Platforms**: See [Remote Deployment Guide](./REMOTE_DEPLOYMENT.md) for general instructions

## Prerequisites

1. **Flow Account**: A Flow account with COA (Contract Owned Account) capabilities

   - See [Flow EVM Gateway Setup](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup) for account creation
   - For testnet: Use [Flow Testnet Faucet](https://faucet.flow.com/)

2. **Deployed Contracts**: ERC-4337 contracts deployed on Flow Testnet

   - EntryPoint: `0xcf1e8398747a05a997e8c964e957e47209bdff08`
   - SimpleAccountFactory: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
   - PaymasterERC20: `0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a`
   - See `docs/FLOW_TESTNET_DEPLOYMENT.md` for complete contract addresses

3. **Build Tools**: Go 1.25+ installed
   - See [Flow EVM Gateway Setup](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup) for build instructions

## Step 1: Build the Gateway

### Option A: Build from Source

```bash
git clone https://github.com/onflow/flow-evm-gateway.git
cd flow-evm-gateway
git checkout $(curl -s https://api.github.com/repos/onflow/flow-evm-gateway/releases/latest | jq -r .tag_name)
CGO_ENABLED=1 go build -o evm-gateway cmd/main/main.go
chmod a+x evm-gateway
```

### Option B: Build with Docker

```bash
git clone https://github.com/onflow/flow-evm-gateway.git
cd flow-evm-gateway
git checkout $(curl -s https://api.github.com/repos/onflow/flow-evm-gateway/releases/latest | jq -r .tag_name)
make docker-build
```

## Step 2: Configure Keys and Secrets

**⚠️ Security Warning**: Never commit private keys to version control!

### Recommended: Use Environment Variables

Create a `.env` file (see `.env.example` template):

```bash
# Create .env file
cp .env.example .env
# Edit .env with your actual values
chmod 600 .env  # Restrict permissions
```

Then source it before running:

```bash
source .env
./evm-gateway run \
  --coa-address="$COA_ADDRESS" \
  --coa-key="$COA_KEY" \
  # ... other flags
```

**See [Key Management Guide](./KEY_MANAGEMENT.md) for detailed security practices.**

## Step 3: Configure ERC-4337

The gateway requires the following ERC-4337 configuration flags:

### Required Configuration

- `--entry-point-address`: Address of the deployed EntryPoint contract
- `--bundler-enabled`: Enable ERC-4337 bundler functionality (set to `true`)

### Optional Configuration

- `--max-ops-per-bundle`: Maximum UserOperations per bundle (default: 10)
- `--user-op-ttl`: Time to live for pending UserOperations (default: 5m)
- `--bundler-beneficiary`: Address to receive bundler fees (optional)
- `--bundler-interval`: Interval at which bundler processes UserOperations (default: 800ms)
  - See [Bundler Interval Decision](./BUNDLER_INTERVAL_DECISION.md) for detailed impact analysis

### Example Configuration for Flow Testnet

```bash
./evm-gateway run \
  --flow-network-id=flow-testnet \
  --access-node-grpc-host=access.testnet.nodes.onflow.org:9000 \
  --coinbase=<YOUR_EVM_COINBASE_ADDRESS> \
  --coa-address=<YOUR_FLOW_COA_ADDRESS> \
  --coa-key=<YOUR_FLOW_COA_PRIVATE_KEY> \
  --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
  --bundler-enabled=true \
  --max-ops-per-bundle=10 \
  --user-op-ttl=5m \
  --bundler-beneficiary=<YOUR_BUNDLER_FEE_RECIPIENT> \
  --bundler-interval=800ms \
  --rpc-port=8545 \
  --ws-enabled=true \
  --metrics-port=9091
```

### Full Example with All Common Flags

```bash
./evm-gateway run \
  --flow-network-id=flow-testnet \
  --access-node-grpc-host=access.testnet.nodes.onflow.org:9000 \
  --access-node-spork-hosts="access-001.testnet15.nodes.onflow.org:9000,access-001.testnet16.nodes.onflow.org:9000" \
  --coinbase=0x3cC530e139Dd93641c3F30217B20163EF8b17159 \
  --coa-address=<16-character-hex-address> \
  --coa-key=<64-character-hex-private-key> \
  --gas-price=0 \
  --entry-point-address=0xcf1e8398747a05a997e8c964e957e47209bdff08 \
  --bundler-enabled=true \
  --max-ops-per-bundle=10 \
  --user-op-ttl=5m \
  --bundler-beneficiary=0x3cC530e139Dd93641c3F30217B20163EF8b17159 \
  --bundler-interval=800ms \
  --rpc-host=0.0.0.0 \
  --rpc-port=8545 \
  --ws-enabled=true \
  --metrics-port=9091 \
  --log-level=info
```

## Step 4: Verify Gateway Startup

### Check Logs

The gateway should start and begin indexing. Look for:

1. **ERC-4337 Initialization**:

   ```
   ERC-4337 bundler enabled
   EntryPoint address: 0xcf1e8398747a05a997e8c964e957e47209bdff08
   ```

2. **Indexing Progress**:

   ```
   evm_gateway_blocks_indexed_total
   evm_gateway_evm_block_height
   ```

3. **API Server Ready**:
   ```
   JSON-RPC server listening on :8545
   ```

### Check Node Status

```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

Expected response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x..."
}
```

## Step 5: Test ERC-4337 Functionality

### Test 1: Check if Bundler is Enabled

```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'
```

Should return Chain ID `545` for Flow Testnet.

### Test 2: Send a UserOperation

Use a wallet that supports ERC-4337 (e.g., Privy, Alchemy, etc.) or send a raw JSON-RPC request:

```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "eth_sendUserOperation",
    "params": [{
      "sender": "0x...",
      "nonce": "0x0",
      "initCode": "0x...",
      "callData": "0x...",
      "callGasLimit": "0x...",
      "verificationGasLimit": "0x...",
      "preVerificationGas": "0x...",
      "maxFeePerGas": "0x...",
      "maxPriorityFeePerGas": "0x...",
      "paymasterAndData": "0x",
      "signature": "0x..."
    }, "0xcf1e8398747a05a997e8c964e957e47209bdff08"],
    "id": 1
  }'
```

### Test 3: Estimate UserOperation Gas

```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "eth_estimateUserOperationGas",
    "params": [{
      "sender": "0x...",
      "nonce": "0x0",
      "callData": "0x...",
      "paymasterAndData": "0x"
    }, "0xcf1e8398747a05a997e8c964e957e47209bdff08"],
    "id": 1
  }'
```

### Test 4: Query UserOperation Receipt

After a UserOperation is executed, query its receipt:

```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "eth_getUserOperationReceipt",
    "params": ["0x<userOpHash>"],
    "id": 1
  }'
```

## Step 6: Monitor Gateway Health

### Prometheus Metrics

The gateway exposes Prometheus metrics on the `--metrics-port` (default: 9091).

**Key Metrics for ERC-4337**:

- `evm_gateway_api_errors_total`: Total API errors
- `evm_gateway_blocks_indexed_total`: Blocks indexed
- `evm_gateway_evm_block_height`: Current EVM block height
- `evm_gateway_operator_balance`: COA account balance
- `evm_gateway_available_signing_keys`: Available signing keys

### Check Metrics

```bash
curl http://localhost:9091/metrics | grep evm_gateway
```

### Monitor Signing Keys

If you see errors like "no signing keys available", add more signing keys to your COA account. See [Account and Key Management](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup#account-and-key-management) in the Flow documentation.

## Step 7: Integration Testing

### Test SimpleAccount Creation

1. **Create a SimpleAccount via UserOperation**:

   - Use SimpleAccountFactory at `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
   - Include `initCode` in UserOperation to call `createAccount(owner, salt)`

2. **Send UserOperation with Native Payment**:

   - UserOperation without paymaster (user pays with native FLOW)

3. **Send UserOperation with PaymasterERC20**:
   - UserOperation with `paymasterAndData` pointing to `0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a`
   - User must approve PaymasterERC20 to spend TEST tokens

### Verify on Block Explorer

Check transactions on [Flow Testnet Explorer](https://evm-testnet.flowscan.io):

- EntryPoint: https://evm-testnet.flowscan.io/address/0xcf1e8398747a05a997e8c964e957e47209bdff08
- SimpleAccountFactory: https://evm-testnet.flowscan.io/address/0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12
- PaymasterERC20: https://evm-testnet.flowscan.io/address/0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a

## Troubleshooting

### Gateway Won't Start

1. **Check COA Account**: Ensure `--coa-address` and `--coa-key` are correct
2. **Check EntryPoint Address**: Verify `--entry-point-address` matches deployed contract
3. **Check Network**: Ensure `--flow-network-id` matches your target network

### UserOperations Failing

1. **Check EntryPoint Deposit**: PaymasterERC20 needs FLOW deposited to EntryPoint
2. **Check Token Balance**: For PaymasterERC20, ensure sufficient TEST token balance
3. **Check Gas Limits**: Verify `verificationGasLimit` and `callGasLimit` are sufficient
4. **Check Signatures**: Ensure UserOperation signatures are valid

### No Signing Keys Available

If you see "no signing keys available":

1. Add more signing keys to your COA account
2. Restart the gateway to pick up new keys
3. See [Account and Key Management](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup#account-and-key-management)

### Database Issues

If you see database version errors:

1. Delete the database directory: `rm -rf ./db`
2. Restart the gateway (it will re-index from the configured start height)

## Next Steps

1. **Fund PaymasterERC20**: Deposit FLOW to EntryPoint for paymaster operations
2. **Test with Real Wallets**: Integrate with wallets that support ERC-4337
3. **Monitor Performance**: Track metrics and adjust `--max-ops-per-bundle` as needed
4. **Scale Signing Keys**: Add more COA signing keys for high-volume deployments

## Additional Resources

- [Flow EVM Gateway Setup](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup): Complete setup guide
- [Flow Testnet Deployment](./FLOW_TESTNET_DEPLOYMENT.md): Contract addresses and configuration
- [ERC-4337 Implementation Plan](../ERC4337_IMPLEMENTATION_PLAN.md): Technical implementation details
- [EntryPoint Deployment Guide](./ENTRYPOINT_DEPLOYMENT.md): EntryPoint contract details

## Support

- **Discord**: Join `#flow-evm` channel on [Flow Discord](https://discord.gg/flow)
- **GitHub Issues**: Report issues on [flow-evm-gateway](https://github.com/onflow/flow-evm-gateway)
