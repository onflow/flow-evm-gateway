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

WORKDIR /flow-evm-gateway

RUN apt-get update

COPY --from=app-builder /app/bin /flow-evm-gateway/app
COPY --from=app-builder /app/previewnet-keys.json /flow-evm-gateway/previewnet-keys.json
COPY --from=app-builder /app/migration-keys.json /flow-evm-gateway/migration-keys.json

EXPOSE 8545

ENTRYPOINT ["/flow-evm-gateway/app"]
