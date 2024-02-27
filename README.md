<img src="https://assets-global.website-files.com/5f734f4dbd95382f4fdfa0ea/65b0115890bbda5c804f7524_donuts%202-p-500.png" alt="evm" width="300"/>

# EVM Gateway

**EVM Gateway enables seamless interaction with the Flow EVM, mirroring the experience of engaging with any other EVM blockchain.**

FlowEVM Gateway implements the Ethereum JSON-RPC API for the [FlowEVM](https://developers.flow.com/evm/about) which conforms to the Ethereum [JSON-RPC specification](https://ethereum.github.io/execution-apis/api-documentation/). FlowEVM Gateway is specifically designed to integrate with the FlowEVM environment on the Flow blockchain. Rather than implementing the full `geth` stack, the JSON-RPC API available in FlowEVM Gateway is a lightweight implementation which uses Flow's underlying consensus and smart contract language, [Cadence](https://cadence-lang.org/docs/), to handle calls received by the FlowEVM Gateway. For those interested in the underlying implementation details please refer to the [FLIP #243](https://github.com/onflow/flips/issues/243) (FlowEVM Gateway) and [FLIP #223](https://github.com/onflow/flips/issues/223) (FlowEVM Core) improvement proposals. 

FlowEVM Gateway is compatible with the majority of standard Ethereum JSON-RPC APIs allowing seamless integration with existing Ethereum-compatible web3 tools via HTTP. FlowEVM Gateway honors Ethereum's JSON-RPC namespace system, grouping RPC methods into categories based on their specific purpose. Each method name is constructed using the namespace, an underscore, and the specific method name in that namespace. For example, the `eth_call` method is located within the `eth` namespace.


# Running
Operating an EVM Gateway is straightforward. It can either be deployed locally alongside the Flow emulator or configured to connect with any active Flow networks supporting EVM. Given that the EVM Gateway depends solely on [Access Node APIs](https://developers.flow.com/networks/node-ops/access-onchain-data/access-nodes/accessing-data/access-api), it is compatible with any networks offering this API access.

### Running Locally
**Start Emulator**

In order to run the gateway locally you need to start the emulator with EVM enabled:
```
flow emulator --evm-enabled
```
_Make sure flow.json has the emulator account configured to address and private key we will use for starting gateway bellow._

Then you need to start the gateway:
```
go run cmd/main/main.go \
  --init-cadence-height 0 \
  --coinbase FACF71692421039876a5BB4F10EF7A439D8ef61E \
  --coa-address f8d6e0586b0a20c7 \
  --coa-key 2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
  --coa-resource-create \
  --gas-price 0
```

_In this example we use `coa-address` value set to service account of the emulator, same as `coa-key`. 
This account will by default be funded with Flow which is a requirement. For `coinbase` we can 
use whichever valid EVM address. It's not really useful for local running beside collecting fees. We provide also the 
`coa-resource-create` to auto-create resources needed on start-up on the `coa` account in order to operate gateway. 
`gas-price` is set at 0 so we don't have to fund EOA accounts. We can set it higher but keep in mind you will then 
need funded accounts for interacting with EVM._

## Configuration Flags

The application can be configured using the following flags at runtime:

| Flag                       | Default Value    | Description                                                                                                            |
|----------------------------|------------------|------------------------------------------------------------------------------------------------------------------------|
| `--database-dir`           | `./db`           | Path to the directory for the database.                                                                                |
| `--rpc-host`               | (empty)          | Host for the JSON RPC API server.                                                                                      |
| `--rpc-port`               | `8545`           | Port for the JSON RPC API server.                                                                                      |
| `--access-node-grpc-host`  | `localhost:3569` | Host to the Flow access node (AN) gRPC API.                                                                            |
| `--init-cadence-height`    | `EmptyHeight`    | Init cadence block height from where the event ingestion will start. *WARNING*: Used only if no existing DB values.    |
| `--evm-network-id`         | `testnet`        | EVM network ID (options: `testnet`, `mainnet`).                                                                        |
| `--flow-network-id`        | `emulator`       | Flow network ID (options: `emulator`, `previewnet`).                                                                   |
| `--coinbase`               | (required)       | Coinbase address to use for fee collection.                                                                            |
| `--gas-price`              | `1`              | Static gas price used for EVM transactions.                                                                            |
| `--coa-address`            | (required)       | Flow address that holds COA account used for submitting transactions.                                                  |
| `--coa-key`                | (required)       | *WARNING*: Do not use this flag in production! Private key value for the COA address used for submitting transactions. |
| `--coa-resource-create`    | `false`          | Auto-create the COA resource in the Flow COA account provided if one doesn't exist.                                    |

## Getting Started

To start using EVM Gateway, ensure you have the required dependencies installed and then run the application with your desired configuration flags. For example:

```bash
./evm-gateway --rpc-host "127.0.0.1" --rpc-port 3000 --database-dir "/path/to/database"
````
For more detailed information on configuration and deployment, refer to the Configuration and Deployment sections.

## Contributing
We welcome contributions from the community! Please read our Contributing Guide for information on how to get involved.

## License
EVM Gateway is released under the Apache License 2.0. See the LICENSE file for more details.

# FlowEVM Gateway Endpoints

FlowEVM has public RPC endpoints available for the following environments:

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | FlowEVM Crescendo PreviewNet           |
| Description     | The public RPC URL for FlowEVM Preview |
| RPC Endpoint    | https://crescendo.evm.nodes.onflow.org |
| Chain ID        | 646                                    |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://crescendo.flowdiver.io         |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | FlowEVM Testnet                        |
| Description     | The public RPC URL for FlowEVM testnet |
| RPC Endpoint    | https://testnet.evm.nodes.onflow.org   |
| Chain ID        | Coming Soon                            |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://testnet.flowdiver.io           |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | FlowEVM Mainnet                        |
| Description     | The public RPC URL for FlowEVM mainnet |
| RPC Endpoint    | https://mainnet.evm.nodes.onflow.org   |
| Chain ID        | 747                                    |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://flowdiver.io                   |