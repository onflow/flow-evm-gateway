# BUILD BIN

FROM golang:1.22.0 as app-builder

# Build the app binary in /app
WORKDIR /app

# Install go modules
COPY go.* ./
COPY . ./

RUN go mod download
RUN go mod verify

# Build binary
RUN CGO_ENABLED=1 go build -o bin cmd/main.go
RUN chmod a+x bin

# RUN APP

FROM debian:latest

WORKDIR /flow-evm-gateway

RUN apt-get update && apt-get install ca-certificates -y

COPY --from=app-builder /app/bin /flow-evm-gateway/app

EXPOSE 8545

ENTRYPOINT ["/flow-evm-gateway/app", "run"]
