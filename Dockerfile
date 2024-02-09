
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


# RUN APP
FROM alpine:latest
WORKDIR /flow-evm-gateway
COPY --from=builder /flow-evm-gateway/evm-gateway /flow-evm-gateway/evm-gateway
EXPOSE 8888
CMD ["./evm-gateway"]
