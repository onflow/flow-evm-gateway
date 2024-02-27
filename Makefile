.PHONY: test
test:
	# test all packages
	go test -cover ./...

.PHONY: e2e-test
e2e-test:
	# test all packages
	cd integration && go test -cover ./...

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
	mockery --all --dir=services/events --output=services/events/mocks

.PHONY: ci
ci: check-tidy test e2e-test

.PHONY: start-emulator
start-emulator:
	./flow-x86_64-linux- emulator --evm-enabled

.PHONY: setup-account
setup-account:
	./flow-x86_64-linux- transactions send api/cadence/transactions/create_bridged_account.cdc 1500.0 --network=emulator --signer=emulator-account

.PHONY: start
start:
	go run ./cmd/server/main.go