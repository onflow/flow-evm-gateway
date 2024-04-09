#!/bin/bash

flow emulator &
sleep 5

FLOW_NETWORK_ID=flow-emulator
COINBASE=FACF71692421039876a5BB4F10EF7A439D8ef61E
COA_ADDRESS=f8d6e0586b0a20c7
COA_KEY=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21
COA_RESOURCE_CREATE=true
GAS_PRICE=0
RPC_HOST=0.0.0.0
RPC_PORT=8545

/app/flow-evm-gateway/evm-gateway --flow-network-id=$FLOW_NETWORK_ID --coinbase=$COINBASE --coa-address=$COA_ADDRESS --coa-key=$COA_KEY --coa-resource-create=$COA_RESOURCE_CREATE --gas-price=$GAS_PRICE --rpc-host=$RPC_HOST --rpc-port=$RPC_PORT
sleep 5