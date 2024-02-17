# FlowEVM Gateway

FlowEVM Gateway implements the Ethereum JSON-RPC API for the [FlowEVM](https://developers.flow.com/evm/about) which conforms to the Ethereum [JSON-RPC specification](https://ethereum.github.io/execution-apis/api-documentation/). FlowEVM Gateway is specifically designed to integrate with the FlowEVM environment on the Flow blockchain. Rather than implementing the full `geth` stack, the JSON-RPC API available in FlowEVM Gateway is a lightweight implementation which uses Flow's underlying consensus and smart contract language, [Cadence](https://cadence-lang.org/docs/), to handle calls received by the FlowEVM Gateway. For those interested in the underlying implementation details please refer to the [FLIP #243](https://github.com/onflow/flips/issues/243) (FlowEVM Gateway) and [FLIP #223](https://github.com/onflow/flips/issues/223) (FlowEVM Core) improvement proposals. 

FlowEVM Gateway is compatible with the majority of standard Ethereum JSON-RPC APIs allowing seamless integration with existing Ethereum-compatible web3 tools via HTTP. FlowEVM Gateway honors Ethereum's JSON-RPC namespace system, grouping RPC methods into categories based on their specific purpose. Each method name is constructed using the namespace, an underscore, and the specific method name in that namespace. For example, the `eth_call` method is located within the `eth` namespace.

Listed below are the JSON-RPC namespaces currently supported by the FlowEVM Gateway:

* `eth`
* `web3`
* `net`

We also plan to add support for the `admin` namespace in the near future.

## Event subscription and filters

FlowEVM Gateway also supports the standard Ethereum JSON-RPC event subscription and filters, enabling callers to subscribe to state logs, blocks or pending transactions changes.

* TODO, add more specifics 

# FlowEVM Gateway Endpoints

FlowEVM has public RPC endpoints available for the following environments:

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | FlowEVM Crescendo PreviewNet           |
| Description     | The public RPC URL for FlowEVM Preview |
| RPC Endpoint    | https://crescendo.evm.nodes.onflow.org |
| Chain ID        | Coming Soon                            |
| Currency Symbol | FLOW                                   |
| Block Explorer  | TBD                                    |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | FlowEVM Testnet                        |
| Description     | The public RPC URL for FlowEVM testnet |
| RPC Endpoint    | https://testnet.evm.nodes.onflow.org   |
| Chain ID        | Coming Soon                            |
| Currency Symbol | FLOW                                   |
| Block Explorer  | TBD                                    |

| Name            | Value                                  |
|-----------------|----------------------------------------|
| Network Name    | FlowEVM Mainnet                        |
| Description     | The public RPC URL for FlowEVM mainnet |
| RPC Endpoint    | https://mainnet.evm.nodes.onflow.org    |
| Chain ID        | Coming Soon                            |
| Currency Symbol | FLOW                                   |
| Block Explorer  | TBD                                    |


# Launch a FlowEVM Gateway node

* TODO

## Configuration

* TODO

## Infrastructure considerations

## Logging and metrics

* TODO  

# Running the FlowEVM Gateway with the Flow CLI

We recommend using the [Flow Emulator](https://github.com/onflow/flow-emulator) with the [Flow CLI](https://docs.onflow.org/flow-cli), a command-line interface for working with Flow, for local development and testing of the FlowEVM Gateway. The Flow CLI incorporates support for running the FlowEVM gateway on the command line together with the emulator. 

## Installation

Follow [these steps](https://github.com/onflow/flow-emulator?tab=readme-ov-file#installation) to install the Flow Emulator and CLI.

## Starting the FlowEVM Gateway locally

To run the gateway locally and interact with it, you can use Docker to build the image and run a container.

Build the image:

```bash
docker build -t onflow/flow-evm-gateway .
```

Run a container:

```bash
docker run -d -p 8545:8545 onflow/flow-evm-gateway
```

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

* TODO
