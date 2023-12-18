.PHONY: test
test:
	# test all packages
	go test -parallel 8 ./...
