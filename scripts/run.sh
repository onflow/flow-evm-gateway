#!/bin/bash

./flow-x86_64-linux- transactions send /flow-evm-gateway/create_bridged_account.cdc 1500.0 --network=$FLOW_NETWORK --signer=emulator-account && ./evm-gateway
