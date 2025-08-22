# The short Git commit hash
SHORT_COMMIT := $(shell git rev-parse --short HEAD)
BRANCH_NAME:=$(shell git rev-parse --abbrev-ref HEAD | tr '/' '-')
# The Git commit hash
COMMIT := $(shell git rev-parse HEAD)
# The tag of the current commit, otherwise empty
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null)
CMD_ARGS :=
# ACCESS_NODE_SPORK_HOSTS are comma separated
TESTNET_ACCESS_NODE_SPORK_HOSTS := access-001.devnet51.nodes.onflow.org:9000
MAINNET_ACCESS_NODE_SPORK_HOSTS := access-001.mainnet25.nodes.onflow.org:9000
EMULATOR_COINBASE := FACF71692421039876a5BB4F10EF7A439D8ef61E
EMULATOR_COA_ADDRESS := f8d6e0586b0a20c7
EMULATOR_COA_KEY := 2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21
UNAME_S := $(shell uname -s)
# Set default values
ARCH :=
OS :=
COMPILER_FLAGS := CGO_ENABLED=1

EMULATOR_ARGS := --flow-network-id=flow-emulator \
		--coinbase=$(EMULATOR_COINBASE) \
		--coa-address=$(EMULATOR_COA_ADDRESS)  \
		--coa-key=$(EMULATOR_COA_KEY)  \
		--wallet-api-key=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
		--gas-price=0 \
		--log-writer=console \
		--tx-state-validation=local-index \
		--profiler-enabled=true \
		--profiler-port=6060 \
		--ws-enabled=true

# Set VERSION from command line, environment, or default to SHORT_COMMIT
VERSION ?= $(SHORT_COMMIT)

# Set IMAGE_TAG from VERSION if not explicitly set
IMAGE_TAG ?= $(VERSION)

# docker container registry
CONTAINER_REGISTRY := us-west1-docker.pkg.dev/dl-flow-devex-production/development
DOCKER_BUILDKIT := 1
DATADIR := /data

# Determine OS and set ARCH
ifeq ($(UNAME_S),Darwin)
	OS := macos
	ARCH := arm64
	COMPILER_FLAGS += CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
else ifeq ($(UNAME_S),Linux)
	OS := linux
	ARCH := amd64
else
	$(error Unsupported operating system: $(UNAME_S))
endif

# Function to check and append required arguments
define check_and_append
$(if $($(2)),\
	$(eval CMD_ARGS += --$(1)=$($(2))),\
	$(error ERROR: $(2) ENV variable is required))
endef

# Default target
.PHONY: all
all: test build

.PHONY: test
test:
	# test all packages
	go test -cover ./...

.PHONY: e2e-test
e2e-test:
	# test all packages
	go clean -testcache
	cd tests/web3js && npm install
	cd tests && LOG_OUTPUT=false go test -cover ./...

.PHONY: check-tidy
check-tidy:
	go mod tidy
	git diff --exit-code
	cd tests
	go mod tidy
	git diff --exit-code

.PHONY: build
build:
	$(COMPILER_FLAGS) go build -o flow-evm-gateway -ldflags="-X github.com/onflow/flow-evm-gateway/api.Version=$(GIT_VERSION)" cmd/main.go
	chmod a+x flow-evm-gateway

.PHONY: fix-lint
fix-lint:
	golangci-lint run -v --fix ./...

.PHONY: generate
generate:
	go install github.com/vektra/mockery/v2@v2.43.2
	mockery --dir=storage --name=BlockIndexer --output=storage/mocks
	mockery --dir=storage --name=ReceiptIndexer --output=storage/mocks
	mockery --dir=storage --name=TransactionIndexer --output=storage/mocks
	mockery --dir=storage --name=TraceIndexer --output=storage/mocks
	mockery --dir=storage --name=FeeParametersIndexer --output=storage/mocks
	mockery --all --dir=services/traces --output=services/traces/mocks
	mockery --all --dir=services/ingestion --output=services/ingestion/mocks
	mockery --dir=models --name=Engine --output=models/mocks

.PHONY: ci
ci: check-tidy test e2e-test

.PHONY: start
start:
	$(COMPILER_FLAGS) go run ./cmd/main.go

.PHONY: start-local
start-local:
	rm -rf db/
	rm -rf metrics/data/
	$(COMPILER_FLAGS) go run cmd/main.go run $(EMULATOR_ARGS)

# Use this after running `make build`, to test out the binary
.PHONY: start-local-bin
start-local-bin:
	rm -rf db/
	rm -rf metrics/data/
	$(COMPILER_FLAGS) go run cmd/main.go run $(EMULATOR_ARGS)

# Build docker image from local sources
.PHONY: docker-build-local
docker-build-local:
ifdef GOARCH
	$(eval ARCH=$(GOARCH))
endif
	docker build --build-arg ARCH=$(ARCH) --no-cache -f dev/Dockerfile -t "$(CONTAINER_REGISTRY)/flow-evm-gateway:$(COMMIT)" .

# Docker run for local development
.PHONY: docker-run-local
docker-run-local:
	@trap 'kill $$(jobs -p)' EXIT
	flow emulator -f dev/flow.json &
	sleep 3

	$(call check_and_append,coinbase,EMULATOR_COINBASE)
	$(call check_and_append,coa-address,EMULATOR_COA_ADDRESS)
	$(call check_and_append,coa-key,EMULATOR_COA_KEY)

	$(eval CMD_ARGS += --flow-network-id=flow-emulator --tx-state-validation=local-index --log-level=debug --gas-price=0 --log-writer=console --profiler-enabled=true --access-node-grpc-host=host.docker.internal:3569)

	docker run -p 8545:8545 --add-host=host.docker.internal:host-gateway "$(CONTAINER_REGISTRY)/flow-evm-gateway:$(COMMIT)" $(CMD_ARGS)


# Build docker image for release
.PHONY: docker-build
docker-build:
ifdef GOARCH
	$(eval ARCH=$(GOARCH))
endif
	docker build --build-arg VERSION="$(VERSION)" --build-arg ARCH=$(ARCH)  -f Dockerfile -t "$(CONTAINER_REGISTRY)/flow-evm-gateway:$(IMAGE_TAG)" \
		--label "git_commit=$(COMMIT)" --label "git_tag=$(IMAGE_TAG)" .

# Install image version from container registry
.PHONY: docker-pull-version
docker-pull-version:
	docker pull "$(CONTAINER_REGISTRY)/flow-evm-gateway:$(IMAGE_VERSION)"

# Run GW image
# https://github.com/onflow/flow-evm-gateway?tab=readme-ov-file#configuration-flags
# Requires the following ENV variables:
#   - ACCESS_NODE_GRPC_HOST: [access.devnet.nodes.onflow.org:9000 | access.mainnet.nodes.onflow.org:9000]
#   - FLOW_NETWORK_ID: [flow-testnet, flow-mainnet]
#   - INIT_CADENCE_HEIGHT: [testnet: 211176670, mainnet: 85981135]
#   - COINBASE: To be set by the operator. This is an EVM EOA or COA address which is set as the receiver of GW transaction fees (remove 0x prefix)
#   - COA_ADDRESS: To be set by the operator. This is a Cadence address which funds gateway operations (remove 0x prefix)
#   - COA_KEY: A full weight, private key belonging to operator COA_ADDRESS (remove 0x prefix). NB: For development use only. We recommend using cloud KMS configuration on mainnet
#
# Optional
#   - GAS_PRICE: the attoFlow amount of gas to charge for transactions
#
# Optional make arguments:
#   - DOCKER_RUN_DETACHED: Runs container in detached mode when true
#   - DOCKER_HOST_PORT: Sets the exposed host port for the gateway JSON-RPC port
#   - DOCKER_HOST_METRICS_PORT: Sets the exposed host port for the gateway metrics RPC port
#   - DOCKER_MOUNT: Sets the host mount point for the EVM data dir
.PHONY: docker-run
docker-run:
	$(eval CMD_ARGS :=)
ifdef DOCKER_RUN_DETACHED
	$(eval MODE=-d)
endif
ifdef DOCKER_HOST_METRICS_PORT
	$(eval HOST_METRICS_PORT=$(DOCKER_HOST_METRICS_PORT))
else
	$(eval HOST_METRICS_PORT=8080)
endif
ifdef DOCKER_HOST_PORT
	$(eval HOST_PORT=$(DOCKER_HOST_PORT))
else
	$(eval HOST_PORT=8545)
endif
ifndef GAS_PRICE
	$(eval GAS_PRICE=100)
endif
ifdef DOCKER_HOST_MOUNT
	$(eval MOUNT=--mount type=bind,src="$(DOCKER_HOST_MOUNT)",target=$(DATADIR))
	$(call check_and_append,database-dir,DATADIR)
endif

ifdef FLOW_NETWORK_ID
ifeq ($(FLOW_NETWORK_ID),flow-testnet)
	$(eval ACCESS_NODE_SPORK_HOSTS=$(TESTNET_ACCESS_NODE_SPORK_HOSTS))
else ifeq ($(FLOW_NETWORK_ID),flow-mainnet)
	$(eval ACCESS_NODE_SPORK_HOSTS=$(MAINNET_ACCESS_NODE_SPORK_HOSTS))
endif
endif

	$(call check_and_append,access-node-grpc-host,ACCESS_NODE_GRPC_HOST)
	$(call check_and_append,flow-network-id,FLOW_NETWORK_ID)
	$(call check_and_append,init-cadence-height,INIT_CADENCE_HEIGHT)
	$(call check_and_append,coinbase,COINBASE)
	$(call check_and_append,coa-address,COA_ADDRESS)
	$(call check_and_append,coa-key,COA_KEY)
	$(call check_and_append,gas-price,GAS_PRICE)
	$(call check_and_append,metrics-port,HOST_METRICS_PORT)

	$(eval CMD_ARGS += --ws-enabled=true --rate-limit=9999999 --rpc-host=0.0.0.0 --log-level=info --tx-state-validation=local-index)
	$(call check_and_append,access-node-spork-hosts,ACCESS_NODE_SPORK_HOSTS)

	docker run $(MODE) -p $(HOST_PORT):8545 -p $(HOST_METRICS_PORT):8080 $(MOUNT) "$(CONTAINER_REGISTRY)/flow-evm-gateway:$(IMAGE_TAG)" $(CMD_ARGS)

