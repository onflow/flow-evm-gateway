package main

import (
	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
)

func main() {
	cfg, err := config.FromFlags()
	if err != nil {
		panic(err)
	}

	err = bootstrap.Start(cfg)
	if err != nil {
		panic(err)
	}
}
