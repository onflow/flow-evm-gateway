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


# Building

We suggest using the docker images we build for you during our CD process. The images can be found at `onflow/flow-evm-gateway`.

**Manual Build**

If you decide to build the binaries yourself you can do so by running:

```
go build -o evm-gateway cmd/main/main.go
```

The binary can then be run using the correct flags (see the table below or the example in the "running" section).
```
./evm-gateway {flags}
```

# Running
Operating an EVM Gateway is straightforward. It can either be deployed locally alongside the Flow emulator or configured to connect with any active Flow networks supporting EVM. Given that the EVM Gateway depends solely on [Access Node APIs](https://developers.flow.com/networks/node-ops/access-onchain-data/access-nodes/accessing-data/access-api), it is compatible with any networks offering this API access.

**Example Configuration for Testnet**
```
./evm-gateway
--access-node-grpc-host https://testnet.evm.nodes.onflow.org \
--flow-network-id flow-testnet \
--coinbase {EVM-account} \
--coa-address {funded Flow account address} \
--coa-key-file {file containing private keys for coa-address, KMS should be used on live networks} \
--init-cadence-height {network Flow height}
--coa-resource-create 
```

### Running Locally
**Start Emulator**

To run the gateway locally you need to start the emulator with EVM enabled:
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

Note that the gateway will be starting from the latest emulator block, so if the emulator is run before and transactions happen in the meantime, the gateway will not fetch those historical blocks & transactions.
This will be improved soon.

_In this example we use `coa-address` value set to the service account of the emulator, the same as `coa-key`. 
This account will by default be funded with Flow which is a requirement. For `coinbase` we can 
use whichever valid EVM address. It's not really useful for local running besides collecting fees. We also allow for the 
`coa-resource-create` to auto-create resources needed on start-up on the `coa` account in order to operate the gateway. 
`gas-price` is set at 0 so we don't have to fund EOA accounts. We can set it higher but keep in mind you will then 
need funded accounts for interacting with EVM._

**With Docker**

Run the following commands:

```bash
cd dev

docker build -t onflow/flow-evm-gateway .

docker run -d -p 127.0.0.1:8545:8545 onflow/flow-evm-gateway
```

**Verify**
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

| Flag                         | Default Value                   | Description                                                                                                                                                          |
|------------------------------|---------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `database-dir`               | `./db`                          | Path to the directory for the database                                                                                                                               |
| `rpc-host`                   | `""`                            | Host for the RPC API server                                                                                                                                          |
| `rpc-port`                   | `8545`                          | Port for the RPC API server                                                                                                                                          |
| `ws-enabled`                 | `false`                         | Enable websocket connections                                                                                                                                         |
| `access-node-grpc-host`      | `localhost:3569`                | Host to the flow access node gRPC API                                                                                                                                |
| `access-node-spork-hosts`    | `""`                            | Previous spork AN hosts, defined as a comma-separated list (e.g. `"host-1.com,host2.com"`)                                                                             |
| `evm-network-id`             | `testnet   `                    | EVM network ID (options: `testnet`, `mainnet`)                                                                                                         |
| `flow-network-id`            | `flow-emulator`                 | Flow network ID (options: `flow-emulator`, `flow-testnet`, `flow-mainnet`)                                                                                       |
| `coinbase`                   | `""`                            | Coinbase address to use for fee collection                                                                                                                           |
| `init-cadence-height`        | `0`                             | Cadence block height to start indexing; avoid using on a new network                                                                                                 |
| `gas-price`                  | `1`                             | Static gas price for EVM transactions                                                                                                                                |
| `coa-address`                | `""`                            | Flow address holding COA account for submitting transactions                                                                                                         |
| `coa-key`                    | `""`                            | Private key for the COA address used for transactions                                                                                                                |
| `coa-key-file`               | `""`                            | Path to a JSON file of COA keys for key-rotation (exclusive with `coa-key` flag)                                                                                     |
| `coa-resource-create`        | `false`                         | Auto-create the COA resource if it doesn't exist in the Flow COA account                                                                                             |
| `coa-cloud-kms-project-id`   | `""`                            | Project ID for KMS keys (e.g. `flow-evm-gateway`)                                                                                                                    |
| `coa-cloud-kms-location-id`  | `""`                            | Location ID for KMS key ring (e.g. 'global')                                                                                                                         |
| `coa-cloud-kms-key-ring-id`  | `""`                            | Key ring ID for KMS keys (e.g. 'tx-signing')                                                                                                                         |
| `coa-cloud-kms-keys`         | `""`                            | KMS keys and versions, comma-separated (e.g. `"gw-key-6@1,gw-key-7@1"`)                                                                                                |
| `log-level`                  | `debug`                         | Log verbosity level (`debug`, `info`, `warn`, `error`, `fatal`, `panic`)                                                                                             |
| `log-writer`                 | `stderr`                        | Output method for logs (`stderr`, `console`)                                                                                                                         |
| `stream-limit`               | `10`                            | Rate-limit for client events sent per second                                                                                                                         |
| `rate-limit`                 | `50`                            | Requests per second limit for clients over any protocol (ws/http)                                                                                                    |
| `address-header`             | `""`                            | Header for client IP when server is behind a proxy                                                                                                                   |
| `heartbeat-interval`         | `100`                           | Interval for AN event subscription heartbeats                                                                                                                        |
| `stream-timeout`             | `3`                             | Timeout in seconds for sending events to clients                                                                                                                     |
| `force-start-height`         | `0`                             | Force-set starting Cadence height (local/testing use only)                                                                                                           |
| `wallet-api-key`             | `""`                            | ECDSA private key for wallet APIs (local/testing use only)                                                                                                           |
| `filter-expiry`              | `5m`                            | Expiry time for idle filters                                                                                                                                         |
| `traces-gcp-bucket`          | `""`                            | GCP bucket name for transaction traces                                                                                                                               |
| `prometheus-config-file-path`| `./metrics/prometheus.yml`      | Path to the Prometheus configuration file                                                                                                                            |
| `index-only`                 | `false`                         | Run in index-only mode, allowing state queries and indexing but no transaction sending                                                                               |

# Deploying
Deploying the EVM Gateway node comes with some prerequisites as well as expectations and they are best explained in the WIP document: https://flowfoundation.notion.site/EVM-Gateway-Deployment-3c41da6710af40acbaf971e22ce0a9fd


# EVM Gateway Endpoints

EVM Gateway has public RPC endpoints available for the following environments:

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | Testnet                                |
| Description     | The public RPC URL for Flow Testnet    |
| RPC Endpoint    | https://testnet.evm.nodes.onflow.org   |
| Chain ID        | 545                                    |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://evm-testnet.flowscan.io        |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | Mainnet                                |
| Description     | The public RPC URL for Flow Mainnet    |
| RPC Endpoint    | https://mainnet.evm.nodes.onflow.org   |
| Chain ID        | 747                                    |
| Currency Symbol | FLOW                                   |
| Block Explorer  | https://evm.flowscan.io                |


# JSON-RPC API
The EVM Gateway implements APIs according to the Ethereum specification: https://ethereum.org/en/developers/docs/apis/json-rpc/#json-rpc-methods

**Additional APIs**
- Tracing APIs allows you to fetch execution traces
    * debug_traceTransaction
    * debug_traceBlockByNumber
    * debug_traceBlockByHash
 
**Unsupported APIs**
- Wallet APIs: we don't officially support wallet APIs (eth_accounts, eth_sign, eth_signTransaction, eth_sendTransaction) due to security
  concerns that come with managing the keys on production environments, however, it is possible to configure the gateway to allow these
  methods for local development by using a special flag `--wallet-api-key`. 
- Proof API: we don't support obtaining proofs yet, Flow piggy-backs on the Flow consensus, and hence the Flow proofs can be used to verify
  and trust the EVM environment. We intend to add access to EVM proofs in the future.
- Access Lists: we don't yet support creating access lists as they don't affect the fees we charge. We might support this in the future
  to optimize fees, but it currently is not part of our priorities. 


# Contributing
We welcome contributions from the community! Please read our [Contributing Guide](./CONTRIBUTING.md) for information on how to get involved.

# License
EVM Gateway is released under the Apache License 2.0 license. See the LICENSE file for more details.
