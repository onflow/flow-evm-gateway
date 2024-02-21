#!/bin/bash
if [ "$FLOW_NETWORK" = "testnet" ] || [ "$FLOW_NETWORK" = "mainnet" ] || [ "$FLOW_NETWORK" = "canary" ] || [ "$FLOW_NETWORK" = "crescendo" ]; then
  ./evm-gateway --network=$FLOW_NETWORK
else 
  # Start the first process & redirect output to a temporary file
  ./flow-x86_64-linux- emulator --evm-enabled > temp_output.txt &

  # PID of the first process
  FIRST_PROCESS_PID=$!

  # Monitor the temporary file for a specific output
  PATTERN="3569"
  while ! grep -q "$PATTERN" temp_output.txt; do
    sleep 1
  done

  # Once the pattern is found, you can kill the first process if needed
  # kill $FIRST_PROCESS_PID

  # Run the second process

  ./flow-x86_64-linux- transactions send /flow-evm-gateway/create_bridged_account.cdc 1500.0 --network=emulator --signer=emulator-account && ./evm-gateway

  # Clean up temporary file
  rm temp_output.txt
fi
