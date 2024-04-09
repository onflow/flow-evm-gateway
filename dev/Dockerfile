FROM ubuntu:latest

# Install curl, Git, and other required dependencies
RUN apt-get update && apt-get install -y curl git

# Download and verify the Go archive
RUN curl -LO https://go.dev/dl/go1.22.2.linux-amd64.tar.gz && \
    echo "5901c52b7a78002aeff14a21f93e0f064f74ce1360fce51c6ee68cd471216a17 go1.22.2.linux-amd64.tar.gz" | sha256sum -c - && \
    tar -C /usr/local -xzf go1.22.2.linux-amd64.tar.gz && \
    rm go1.22.2.linux-amd64.tar.gz

# Add Go binary directory to the PATH environment variable
ENV PATH="/usr/local/go/bin:${PATH}"

# Install Flow CLI using the installation script
RUN sh -ci "$(curl -fsSL https://raw.githubusercontent.com/onflow/flow-cli/master/install.sh)" -- v1.17.0-cadence-v1.0.0-preview.18

# Set the PATH environment variable to include the directory where Flow CLI is installed
ENV PATH="/root/.local/bin:${PATH}"

# Clone the flow-evm-gateway repository
RUN git clone https://github.com/onflow/flow-evm-gateway.git /app/flow-evm-gateway

RUN cd /app/flow-evm-gateway && go mod download
RUN cd /app/flow-evm-gateway && go mod verify
RUN cd /app/flow-evm-gateway && CGO_ENABLED=0 go build -o /app/flow-evm-gateway/evm-gateway /app/flow-evm-gateway/cmd/main/main.go
RUN chmod a+x /app/flow-evm-gateway/evm-gateway

# Copy the flow.json file to the directory where the gateway expects to find it
COPY flow.json /app/flow-evm-gateway/
COPY run.sh /app/flow-evm-gateway/
RUN chmod a+x /app/flow-evm-gateway/run.sh

WORKDIR /app/flow-evm-gateway

EXPOSE 8545

CMD ./run.sh