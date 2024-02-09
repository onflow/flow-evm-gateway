
# BUILD BIN

FROM golang:1.22.0 as builder
# Install go modules
WORKDIR /flow-evm-gateway
COPY go.* ./
COPY . ./
RUN go mod download
RUN go mod verify
RUN CGO_ENABLED=0 go build -o evm-gateway ./cmd/server/main.go
RUN chmod a+x evm-gateway
RUN git clone https://github.com/m-Peter/flow-cli-custom-builds.git

# RUN APP
FROM alpine:latest
WORKDIR /flow-evm-gateway
COPY --from=builder /flow-evm-gateway/evm-gateway /flow-evm-gateway/evm-gateway
COPY --from=builder /flow-evm-gateway/flow-cli-custom-builds/flow-x86_64-linux- /flow-evm-gateway/flow-x86_64-linux-
COPY --from=builder /flow-evm-gateway/flow.json /flow-evm-gateway/flow.json 
COPY --from=builder /flow-evm-gateway/api/cadence/transactions/create_bridged_account.cdc /flow-evm-gateway/create_bridged_account.cdc 
EXPOSE 8545
CMD ./flow-x86_64-linux- emulator --evm-enabled & ./flow-x86_64-linux- transactions send ./create_bridged_account.cdc 1500.0 --network=emulator --signer=emulator-account & ./evm-gateway && wait
