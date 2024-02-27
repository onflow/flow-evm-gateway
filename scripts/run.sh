#!/bin/bash
if [ "$FLOW_NETWORK" = "testnet" ] || [ "$FLOW_NETWORK" = "mainnet" ] || [ "$FLOW_NETWORK" = "canarynet" ] || [ "$FLOW_NETWORK" = "crescendo" ]; then
  ./evm-gateway --network=$FLOW_NETWORK
else
## from https://cloud.google.com/run/docs/tutorials/network-filesystems-filestore
# set -eo pipefail

  # modprobe nfs
# Create mount directory for service.
  # mkdir -p $MNT_DIR

  # echo "Mounting Cloud Filestore."
# echo $FILESTORE_MOUNT_POINT
  # echo $MNT_DIR
# mount -o nolock $FILESTORE_MOUNT_POINT $MNT_DIR
# echo "Mounting completed."

  ./evm-gateway --access-node-grpc-host access-001.previewnet1.nodes.onflow.org:9000 \
    --init-cadence-height 6479 \
    --flow-network-id previewnet \
    --coinbase FACF71692421039876a5BB4F10EF7A439D8ef61E \
    --coa-address 0xaa6d2abaf5bfc626 \
    --coa-key 555b8ccf5dd26d4fa575221791305481d3b23e47e3e1883001eba36d6b5e65ae \
    --coa-resource-create \
    --gas-price 0
fi