.PHONY: test
test:
	# test all packages
	go test -cover ./...

.PHONY: e2e-test
e2e-test:
	# test all packages
	go clean -testcache
	cd tests/web3js && npm install
	cd tests && go test -cover ./...

.PHONY: check-tidy
check-tidy:
	go mod tidy
	git diff --exit-code

.PHONY: generate
generate:
	go get -d github.com/vektra/mockery/v2@v2.21.4
	mockery --dir=storage --name=BlockIndexer --output=storage/mocks
	mockery --dir=storage --name=ReceiptIndexer --output=storage/mocks
	mockery --dir=storage --name=TransactionIndexer --output=storage/mocks
	mockery --dir=storage --name=AccountIndexer --output=storage/mocks
	mockery --dir=storage --name=TraceIndexer --output=storage/mocks
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
	go run cmd/main/main.go --flow-network-id=flow-emulator --coinbase=FACF71692421039876a5BB4F10EF7A439D8ef61E --coa-address=f8d6e0586b0a20c7 --coa-key=2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21 --coa-resource-create=true --gas-price=0
