package main

import (
	"context"
	"fmt"
	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, err := config.FromFlags()
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	err = bootstrap.Start(ctx, cfg)
	if err != nil {
		panic(err)
	}

	osSig := make(chan os.Signal, 1)
	signal.Notify(osSig, syscall.SIGINT, syscall.SIGTERM)

	<-osSig
	fmt.Println("OS Signal to shutdown received, shutting down")
	cancel()
}
