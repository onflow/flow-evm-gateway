#!/bin/bash

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
  --init-cadence-height 15760 \
  --flow-network-id previewnet \
  --coinbase FACF71692421039876a5BB4F10EF7A439D8ef61E \
  --coa-address 0x47ea2d585be47c7c \
  --coa-key 3588afb4f47fd68242478c30aedef7f81a8c71f7d4213460f81aeab771f2e4a3 \
  --coa-resource-create
  --gas-price 0
