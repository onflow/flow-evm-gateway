<img src="https://assets-global.website-files.com/5f734f4dbd95382f4fdfa0ea/65b0115890bbda5c804f7524_donuts%202-p-500.png" alt="evm" width="300"/>

# EVM Gateway

**EVM Gateway enables seamless interaction with EVM on Flow, mirroring the experience of engaging with any other EVM blockchain.**

EVM Gateway implements the Ethereum JSON-RPC API for [EVM on Flow](https://developers.flow.com/evm/about) which conforms to the Ethereum [JSON-RPC specification](https://ethereum.github.io/execution-apis/api-documentation/). EVM Gateway is specifically designed to integrate with the EVM environment on the Flow blockchain. Rather than implementing the full `geth` stack, the JSON-RPC API available in EVM Gateway is a lightweight implementation which uses Flow's underlying consensus and smart contract language, [Cadence](https://cadence-lang.org/docs/), to handle calls received by the EVM Gateway. For those interested in the underlying implementation details please refer to the [FLIP #243](https://github.com/onflow/flips/issues/243) (EVM Gateway) and [FLIP #223](https://github.com/onflow/flips/issues/223) (EVM on Flow Core) improvement proposals. 

EVM Gateway is compatible with the majority of standard Ethereum JSON-RPC APIs allowing seamless integration with existing Ethereum-compatible web3 tools via HTTP. EVM Gateway honors Ethereum's JSON-RPC namespace system, grouping RPC methods into categories based on their specific purpose. Each method name is constructed using the namespace, an underscore, and the specific method name in that namespace. For example, the `eth_call` method is located within the `eth` namespace. See below for details on methods currently supported or planned.

### Design

![design ](https://github.com/onflow/flow-evm-gateway/assets/75445744/3fd65313-4041-46d1-b263-b848640d019f)


The basic design of the EVM Gateway consists of a couple of components:

- Event Ingestion Engine: this component listens to all Cadence events that are emitted by the EVM core, which can be identified by the special event type ID `evm.TransactionExecuted` and `evm.BlockExecuted` and decodes and index the data they contain in the payloads.
- Flow Requester: this component knows how to submit transactions to Flow AN to change the EVM state. What happens behind the scenes is that EVM gateway will receive an EVM transaction payload, which will get wrapped in a Cadence transaction that calls EVM contract with that payload and then the EVM core will execute the transaction and change the state.
- JSON RPC API: this is the client API component that implements all the API according to the JSON RPC API specification.

## Event subscription and filters

EVM Gateway also supports the standard Ethereum JSON-RPC event subscription and filters, enabling callers to subscribe to state logs, blocks or pending transactions changes.

* TODO more coming

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
  --flow-network-id flow-emulator \
  --coinbase FACF71692421039876a5BB4F10EF7A439D8ef61E \
  --coa-address f8d6e0586b0a20c7 \
  --coa-key 2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
  --coa-resource-create \
  --gas-price 0
```

Note that the gateway will be starting from the latest emulator block, so if emulator is run before and transactions happen in the meantime, the gateway will not fetch those historical blocks & transactions.
This will be improved soon.

_In this example we use `coa-address` value set to service account of the emulator, same as `coa-key`. 
This account will by default be funded with Flow which is a requirement. For `coinbase` we can 
use whichever valid EVM address. It's not really useful for local running beside collecting fees. We provide also the 
`coa-resource-create` to auto-create resources needed on start-up on the `coa` account in order to operate gateway. 
`gas-price` is set at 0 so we don't have to fund EOA accounts. We can set it higher but keep in mind you will then 
need funded accounts for interacting with EVM._

**With Docker**

Run the following commands:

```bash
cd dev

docker build -t onflow/flow-evm-gateway .

docker run -d -p 127.0.0.1:8545:8545 onflow/flow-evm-gateway
```

To verify the service is up and running:

```bash
curl -XPOST 'localhost:8545'  --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

it should return:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x2"
}
```

## Configuration Flags

The application can be configured using the following flags at runtime:

| Flag                        | Default Value    | Description                                                                                                                                     |
|-----------------------------|------------------|-------------------------------------------------------------------------------------------------------------------------------------------------|
| `--database-dir`            | `./db`           | Path to the directory for the database.                                                                                                         |
| `--rpc-host`                | `localhost`      | Host for the JSON RPC API server.                                                                                                               |
| `--rpc-port`                | `8545`           | Port for the JSON RPC API server.                                                                                                               |
| `--access-node-grpc-host`   | `localhost:3569` | Host to the current spork Flow access node (AN) gRPC API.                                                                                       |
| `--access-node-spork-hosts` |                  | Previous spork AN hosts, defined following the schema: `{latest height}@{host}` as comma separated list (e.g. `"200@host-1.com,300@host2.com"`) |
| `--evm-network-id`          | `testnet`        | EVM network ID (options: `testnet`, `mainnet`).                                                                                                 |
| `--flow-network-id`         | `emulator`       | Flow network ID (options: `emulator`, `previewnet`).                                                                                            |
| `--coinbase`                | (required)       | Coinbase address to use for fee collection.                                                                                                     |
| `--init-cadence-height`     | 0                | Define the Cadence block height at which to start the indexing.                                                                                 |
| `--gas-price`               | `1`              | Static gas price used for EVM transactions.                                                                                                     |
| `--coa-address`             | (required)       | Flow address that holds COA account used for submitting transactions.                                                                           |
| `--coa-key`                 | (required)       | *WARNING*: Do not use this flag in production! Private key value for the COA address used for submitting transactions.                          |
| `--coa-key-file`            |                  | File path that contains JSON array of COA keys used in key-rotation mechanism, this is exclusive with `coa-key` flag.                           |
| `--coa-resource-create`     | `false`          | Auto-create the COA resource in the Flow COA account provided if one doesn't exist.                                                             |
| `--log-level`               | `debug`          | Define verbosity of the log output ('debug', 'info', 'error')                                                                                   |
| `--stream-limit`            | 10               | Rate-limits the events sent to the client within one second                                                                                     |
| `--stream-timeout`          | 3sec             | Defines the timeout in seconds the server waits for the event to be sent to the client                                                          |
| `--filter-expiry`           | `5m`             | Filter defines the time it takes for an idle filter to expire                                                                                   |

## Getting Started

To start using EVM Gateway, ensure you have the required dependencies installed and then run the application with your desired configuration flags. For example:

```bash
./evm-gateway --rpc-host "127.0.0.1" --rpc-port 3000 --database-dir "/path/to/database"
````
For more detailed information on configuration and deployment, refer to the Configuration and Deployment sections.

# EVM Gateway Endpoints

EVM Gateway has public RPC endpoints available for the following environments:

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | Previewnet                             |
| Description     | The public RPC URL for Flow Previewnet |
| RPC Endpoint    | https://previewnet.evm.nodes.onflow.org|
| Chain ID        | 646                                    |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://previewnet.flowdiver.io        |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | Testnet                                |
| Description     | The public RPC URL for Flow Testnet    |
| RPC Endpoint    | https://testnet.evm.nodes.onflow.org   |
| Chain ID        | Coming Soon                            |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://testnet.flowdiver.io           |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | Mainnet                                |
| Description     | The public RPC URL for Flow Mainnet    |
| RPC Endpoint    | https://mainnet.evm.nodes.onflow.org   |
| Chain ID        | 747                                    |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://flowdiver.io                   |

# Supported namespaces and methods

Listed below are the JSON-RPC namespaces and methods currently supported by the EVM Gateway:

* `eth`
  * Supported 
    * eth_chainId
    * eth_blockNumber
    * eth_coinbase
    * eth_getLogs
    * eth_getTransactionCount
    * eth_getTransactionReceipt
    * eth_getBlockByNumber
    * eth_call
    * eth_sendRawTransaction
    * eth_getTransactionByHash
    * eth_getCode
    * eth_gasPrice 
    * eth_getBalance
    * eth_estimateGas
    * eth_getTransactionByBlockNumberAndIndex
    * eth_getTransactionByBlockHashAndIndex
    * eth_getBlockByHash
    * eth_getBlockReceipts
    * eth_getBlockTransactionCountByHash
    * eth_getBlockTransactionCountByNumber
    * eth_newFilter
    * eth_uninstallFilter
    * eth_getFilterChanges
    * eth_newBlockFilter
    * eth_newPendingTransactionFilter
    * eth_getUncleCountByBlockHash // return 0
    * eth_getUncleCountByBlockNumber // return 0
    * eth_syncing // return false for now
  * Unsupported but coming soon
    * eth_createAccessList
    * eth_feeHistory
    * eth_maxPriorityFeePerGas
    * eth_getProof
    * eth_getStorageAt
    * eth_getFilterLogs
    * eth_accounts
    * eth_sign
    * eth_signTransaction
    * eth_sendTransaction
* `web3`
  * Supported
    * web3_clientVersion
    * web_sha3
* `net`
  * Supported
    * net_listening
    * net_peerCount
    * net_version

We also plan to add support for the `admin` namespace in the near future.

# Example queries

Following is a list of pre-made JSON-RPC API calls, using `curl`:

## eth_sendRawTransaction

Deploy a smart contract:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0xb9015f02f9015b82029a80808083124f808080b901086060604052341561000f57600080fd5b60eb8061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063c6888fa1146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60007f24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da600783026040518082815260200191505060405180910390a16007820290509190505600a165627a7a7230582040383f19d9f65246752244189b02f56e8d0980ed44e7a56c0b200458caad20bb0029c001a0c0dfc8ed68e5e2522006ac4dc7b1c0c783328311b3e860658be1bab16728ed98a048ef3ec7ff2f13ebda39f01526254e3c7ab64feb0c574703555dba316b6a88e3"],"id":1}'
```

Run some smart contract function calls:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0xb88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb"],"id":2}'
```

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0xb88c02f88982029a02808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000007c080a0409f5baa7622655f76f7f67c1bd8cc4cdd5a8e64fab881d78c47d9d73e907befa04981b65452d66beeb80527f3e8d3d0bf18aa21cfbd9a0c5ee749192d43d72d95"],"id":3}'
```

## eth_call

Make a read-only smart contract function call:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_call","params":[{"from":"0x658bdf435d810c91414ec09147daa6db62406379","to":"0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2","gas":"0x76c0","gasPrice":"0x9184e72a000","value":"0x0","input":"0xc6888fa10000000000000000000000000000000000000000000000000000000000000006"}],"id":4}'
```

## eth_getLogs

Retrieve EVM logs based on a topic:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"topics":["0x24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da"]}],"id":5}'
```

## eth_getTransactionCount

Retrieve the nonce (transaction count) of an EVM address:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getTransactionCount","params":["0x658Bdf435d810C91414eC09147DAA6DB62406379","latest"],"id":6}'
```

## eth_getBlockByNumber

Retrieve block info given its number:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x3",false],"id":7}'
```

## eth_getBlockByHash

Retrieve block info given its hash:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getBlockByHash","params":["0xf0cb48c7561fb6cab8b065b12bf4cf966a6840b41db81055bf4cd98d2354228f",false],"id":8}'
```

## eth_getTransactionReceipt

Retrieve the transaction receipt given its hash:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0xaae4530246e61ae58479824ab0863f99ca50414d27aec0c269ae6a7cfc4c7f5b"],"id":9}'
```

## eth_getTransactionByHash

Retrieve transaction info given its hash:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getTransactionByHash","params":["0xaae4530246e61ae58479824ab0863f99ca50414d27aec0c269ae6a7cfc4c7f5b"],"id":10}'
```

## eth_getBalance

Retrieve the balance of an EVM address:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x000000000000000000000002f239b1ccbbaa9977","latest"],"id":11}'
```

## eth_getBlockTransactionCountByHash

Retrieve the number of transactions in a block, given its hash:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getBlockTransactionCountByHash","params":["0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c"],"id":12}'
```

## eth_getBlockTransactionCountByNumber

Retrieve the number of transactions in a block, given its number:

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getBlockTransactionCountByNumber","params":["0x2"],"id":13}'
```

## eth_getTransactionByBlockHashAndIndex

Retrieve transaction info given its block hash and index

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getTransactionByBlockHashAndIndex","params":["0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c","0x0"],"id":14}'
```

## eth_getTransactionByBlockNumberAndIndex

Retrieve transaction info given its block number and index

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getTransactionByBlockNumberAndIndex","params":["0x3","0x0"],"id":15}'
```

## eth_getBlockReceipts

Retrieve all the transaction receipts given a block number

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_getBlockReceipts","params":["0x3"],"id":16}'
```

## eth_blockNumber

Retrieve the latest block number

```bash
curl -XPOST 'localhost:8545' --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":17}'
```

# Contributing
We welcome contributions from the community! Please read our [Contributing Guide](./CONTRIBUTING.md) for information on how to get involved.

# License
EVM Gateway is released under the Apache License 2.0 license. See the LICENSE file for more details.
