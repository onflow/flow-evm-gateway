
# BUILD BIN

FROM golang:1.22.0 as builder
# Install go modules
WORKDIR /flow-evm-gateway
COPY go.* ./
COPY . ./
RUN go mod download
RUN go mod verify
RUN CGO_ENABLED=0 go build -o evm-gateway ./cmd/main/main.go
RUN chmod a+x evm-gateway
RUN chmod a+x ./scripts/run.sh

# RUN APP
FROM debian:latest
WORKDIR /flow-evm-gateway
ENV MNT_DIR /flow-evm-gateway/db
RUN apt-get update
RUN apt-get install -y nfs-common
COPY --from=builder /flow-evm-gateway/evm-gateway /flow-evm-gateway/evm-gateway
COPY --from=builder /flow-evm-gateway/scripts/run.sh /flow-evm-gateway/run.sh
EXPOSE 3000
CMD cd /flow-evm-gateway && ./run.sh
