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
	CGO_ENABLED=1 go build -o flow-evm-gateway -ldflags="-X github.com/onflow/flow-evm-gateway/api.Version=$(shell git describe --tags --abbrev=0 2>/dev/null || echo 'unknown')" cmd/main.go
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
