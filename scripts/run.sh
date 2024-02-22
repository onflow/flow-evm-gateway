#!/bin/bash

## from https://cloud.google.com/run/docs/tutorials/network-filesystems-filestore
set -eo pipefail

MNT_DIR='./db'

# Create mount directory for service.
mkdir -p $MNT_DIR

echo "Mounting Cloud Filestore."
echo $FILESTORE_MOUNT_POINT
echo $MNT_DIR
mount -o nolock $FILESTORE_MOUNT_POINT $MNT_DIR
echo "Mounting completed."

./evm-gateway --access-node-grpc-host access-001.previewnet1.nodes.onflow.org:9000 \
  --init-cadence-height 93680 \
  --flow-network-id previewnet \
  --coinbase FACF71692421039876a5BB4F10EF7A439D8ef61E \
  --coa-address 0xa8a7a61c869b028a \
  --coa-key 290139481555cd366e1bc594c2981941af29e8ea7fbc4e21796ff62db415df1c \
  --coa-resource-create
