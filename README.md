# FlowEVM Gateway

FlowEVM Gateway implements the Ethereum JSON-RPC API for the [FlowEVM](https://developers.flow.com/evm/about) which conforms to the Ethereum [JSON-RPC specification](https://ethereum.github.io/execution-apis/api-documentation/). FlowEVM Gateway is specifically designed to integrate with the FlowEVM environment on the Flow blockchain. Rather than implementing the full `geth` stack, the JSON-RPC API available in FlowEVM Gateway is a lightweight implementation which uses Flow's underlying consensus and smart contract language, [Cadence](https://cadence-lang.org/docs/), to handle calls recieved by the FlowEVM Gateway. For those interested in the underlying implementation details please refer to the FlowEVM Gateway [FLIP #243](https://github.com/onflow/flips/issues/243) and FlowEVM Core [FLIP #223](https://github.com/onflow/flips/issues/223) improvement proposals. 

FlowEVM Gateway is compatible with the majority of standard Ethereum JSON-RPC APIs allowing seamless integration with existing Ethereum-compatible web3 tools via HTTP. FlowEVM Gateway honors Ethereum's JSON-RPC namespace system, grouping RPC methods into categories based on their specific purpose. Each method name is constructed using the namespace, an underscore, and the actual method name within that namespace. For instance, the `eth_call` method is located within the `eth` namespace.

Listed below are the JSON-RPC namespaces currently supported by the FlowEVM Gateway:

* `eth`
* `web3`
* `net`

We also plan to add support for the `admin` namespace in the near future.

## Event subscription and filters

FlowEVM Gateway also supports the standard Ethereum JSON-RPC event subscription and filters, enabling callers to subscribe to state logs, blocks or pending transactions changes.

* TODO, add more specifics 

# Launch a FlowEVM Gateway node

* TODO

## Configuration

* TODO

## Infrastructure considerations

## Logging and metrics

* TODO  

# Running the FlowEVM Gateway with the FLow CLI

We recommend using the [Flow Emulator](https://github.com/onflow/flow-emulator) with the [Flow CLI](https://docs.onflow.org/flow-cli), a command-line interface for working with Flow, for local development and testing of the FlowEVM Gateway. The Flow CLI incorporates support for running the FlowEVM gateway on the command line together with the emulator. 

## Installation

Follow [these steps](https://github.com/onflow/flow-emulator?tab=readme-ov-file#installation) to install the Flow Emulator and CLI.

## Starting the server from the CLI

* TODO

# Running the FlowEVM Gateway in Docker

* TODO

# Contributing

* TODO
