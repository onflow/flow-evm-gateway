# The short Git commit hash
SHORT_COMMIT := $(shell git rev-parse --short HEAD)
BRANCH_NAME:=$(shell git rev-parse --abbrev-ref HEAD | tr '/' '-')
# The Git commit hash
COMMIT := $(shell git rev-parse HEAD)
# The tag of the current commit, otherwise empty
GIT_VERSION := $(shell git describe --tags --abbrev=2 2>/dev/null)

# Image tag: if image tag is not set, set it with version (or short commit if empty)
ifeq (${IMAGE_TAG},)
IMAGE_TAG := ${VERSION}
endif

ifeq (${IMAGE_TAG},)
IMAGE_TAG := ${SHORT_COMMIT}
endif

VERSION ?= ${IMAGE_TAG}

ifeq ($(origin VERSION),command line)
    VERSION = $(VERSION)
endif

# docker container registry
export CONTAINER_REGISTRY := gcr.io/flow-container-registry
export DOCKER_BUILDKIT := 1

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
	CGO_ENABLED=1 go build -o flow-evm-gateway -ldflags="-X github.com/onflow/flow-evm-gateway/api.Version=$(IMAGE_TAG)" cmd/main.go
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
	mockery --dir=storage --name=AccountIndexer --output=storage/mocks
	mockery --dir=storage --name=TraceIndexer --output=storage/mocks
	mockery --all --dir=services/traces --output=services/traces/mocks
	mockery --all --dir=services/ingestion --output=services/ingestion/mocks
	mockery --dir=models --name=Engine --output=models/mocks

.PHONY: ci
ci: check-tidy test e2e-test

.PHONY: start
start:
	go run ./cmd/server/main.go

.PHONY: start-local
start-local:
	rm -rf db/
	rm -rf metrics/data/
	go run cmd/main.go run \
		--flow-network-id=flow-emulator \
		--coinbase=FACF71692421039876a5BB4F10EF7A439D8ef61E \
		--coa-address=f8d6e0586b0a20c7 \
		--coa-key=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
		--wallet-api-key=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
		--coa-resource-create=true \
		--gas-price=0 \
		--log-writer=console \
		--profiler-enabled=true \
		--profiler-port=6060

# Use this after running `make build`, to test out the binary
.PHONY: start-local-bin
start-local-bin:
	rm -rf db/
	rm -rf metrics/data/
	./flow-evm-gateway run \
		--flow-network-id=flow-emulator \
		--coinbase=FACF71692421039876a5BB4F10EF7A439D8ef61E \
		--coa-address=f8d6e0586b0a20c7 \
		--coa-key=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
		--wallet-api-key=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 \
		--coa-resource-create=true \
		--gas-price=0 \
		--log-writer=console \
		--profiler-enabled=true \
		--profiler-port=6060

# Build docker image
.PHONY: docker-build
docker-build:
	docker build --build-arg VERSION="$(VERSION)" -f Dockerfile -t "$(CONTAINER_REGISTRY)/evm-gateway:$(IMAGE_TAG)" \
		--label "git_commit=$(COMMIT)" --label "git_tag=$(IMAGE_TAG)" .

# Run GW image
# https://github.com/onflow/flow-evm-gateway?tab=readme-ov-file#configuration-flags
#
# Uses ENV variables to configure below
.PHONY: docker-run
docker-run:
	docker run "$(CONTAINER_REGISTRY)/evm-gateway:$(IMAGE_TAG)" --access-node-grpc-host=$(ACCESS_NODE_GRPC_HOST) --flow-network-id=$(FLOW_NETWORK_ID) \
		--init-cadence-height=$(INIT_CADENCE_HEIGHT) --ws-enabled=true --coinbase=$(COINBASE) --coa-address=$(COA_ADDRESS) \
		--coa-key=$(COA_KEY) --rate-limit=9999999 --rpc-host=0.0.0.0