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

	boot, err := bootstrap.New(cfg)

	if cfg.TracesEnabled {
		if err := boot.StartTraceDownloader(ctx); err != nil {
			panic(err)
		}
	}

	if err := boot.StartEventIngestion(ctx); err != nil {
		panic(err)
	}

	if err := boot.StartAPIServer(ctx); err != nil {
		panic(err)
	}

	if err := boot.StartMetricsServer(ctx); err != nil {
		panic(err)
	}

	osSig := make(chan os.Signal, 1)
	signal.Notify(osSig, syscall.SIGINT, syscall.SIGTERM)

	<-osSig
	fmt.Println("OS Signal to shutdown received, shutting down")

	boot.StopEventIngestion()
	boot.StopMetricsServer()
	boot.StopEventIngestion()
	boot.StopTraceDownloader()

	cancel()
}
