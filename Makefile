.PHONY: test
test:
	# test all packages
	go test -cover -parallel 8 ./...

.PHONY: check-tidy
check-tidy:
	go mod tidy
	git diff --exit-code

.PHONY: generate
generate:
	go get -d github.com/vektra/mockery/v2@v2.21.4
	mockery --all --dir=storage --output=storage/mocks
	mockery --name=FlowAccessClient --dir=api/mocksiface --structname=MockAccessClient --output=api/mocks

.PHONY: ci
ci: check-tidy test

.PHONY: start-emulator
start-emulator:
	./flow-x86_64-linux- emulator --evm-enabled

.PHONY: setup-account
setup-account:
	./flow-x86_64-linux- transactions send api/cadence/transactions/create_bridged_account.cdc 1500.0 --network=emulator --signer=emulator-account

.PHONY: start
start:
	go run ./cmd/server/main.go
