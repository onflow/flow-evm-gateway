package main

import (
	"github.com/onflow/flow-evm-gateway/cmd"
	"github.com/onflow/flow-evm-gateway/config"
)

func main() {
	cfg, err := config.FromFlags()
	if err != nil {
		panic(err)
	}

	err = cmd.Start(cfg)
	if err != nil {
		panic(err)
	}
}
