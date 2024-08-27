package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
)

func main() {
	// create multi-key account
	if _, exists := os.LookupEnv("MULTIKEY_MODE"); exists {
		bootstrap.RunCreateMultiKeyAccount()
		return
	}

	cfg, err := config.FromFlags()
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		osSig := make(chan os.Signal, 1)
		signal.Notify(osSig, syscall.SIGINT, syscall.SIGTERM)

		<-osSig
		fmt.Println("OS Signal to shutdown received, shutting down")
		cancel()
	}()

	bootstrap.Run(ctx, cfg)
}
