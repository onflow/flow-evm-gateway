# BUILD BIN

FROM golang:1.20.0 as app-builder

# Build the app binary in /app
WORKDIR /app

# Install go modules
COPY go.* ./
COPY . ./

RUN go mod download
RUN go mod verify

# Build binary
RUN CGO_ENABLED=1 go build -o bin ./cmd/main/main.go
RUN chmod a+x bin

# RUN APP

FROM debian:latest

ENV ACCESS_NODE_GRPC_HOST=""
ENV RPC_PORT=""
ENV FLOW_NETWORK_ID=""
ENV COINBASE=""
ENV COA_ADDRESS=""
ENV COA_KEY_FILE=""
ENV COA_RESOURCE_CREATE=""
ENV GAS_PRICE=""
ENV INIT_CADENCE_HEIGHT=""

WORKDIR /flow-evm-gateway

RUN apt-get update

COPY --from=app-builder /app/bin /flow-evm-gateway/app
COPY --from=app-builder /app/previewnet-keys.json /flow-evm-gateway/previewnet-keys.json

EXPOSE 8545

ENTRYPOINT /flow-evm-gateway/app -access-node-grpc-host=$ACCESS_NODE_GRPC_HOST \
    -rpc-port=$RPC_PORT \
    -flow-network-id=$FLOW_NETWORK_ID \
    -coinbase=$COINBASE \
    -coa-address=$COA_ADDRESS \
    -coa-key-file=$COA_KEY_FILE \
    -coa-resource-create=$COA_RESOURCE_CREATE \
    -gas-price=$GAS_PRICE \
    -init-cadence-height=$INIT_CADENCE_HEIGHT
