.PHONY: test
test:
	# test all packages
	go test -cover -parallel 8 ./...

.PHONY: check-tidy
check-tidy:
	go mod tidy
	git diff --exit-code

.PHONY: ci
ci: check-tidy test
