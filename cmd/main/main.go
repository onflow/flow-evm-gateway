package main

import (
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/rs/zerolog"
)

const (
	defaultAccessURL = grpc.EmulatorHost
	coinbaseAddr     = "0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"
)

func main() {
	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	// start server
	go func() {
		runServer(logger)
	}()

	// start ingestion
	cfg, err := configFromFlags()
	if err != nil {
		logger.Fatal().Err(err)
	}

	err = start(cfg)
	if err != nil {
		logger.Fatal().Err(err)
	}
}

func runServer(logger zerolog.Logger) {
	var network, coinbase string

	flag.StringVar(&network, "network", "testnet", "network to connect the gateway to")
	flag.StringVar(&coinbase, "coinbase", coinbaseAddr, "coinbase address to use for fee collection")
	flag.Parse()

	config := &api.Config{}
	config.Coinbase = common.HexToAddress(coinbase)
	if network == "testnet" {
		config.ChainID = api.FlowEVMTestnetChainID
	} else if network == "mainnet" {
		config.ChainID = api.FlowEVMMainnetChainID
	} else {
		panic(fmt.Errorf("unknown network: %s", network))
	}

	store := storage.NewStore()

	logger = logger.With().Str("component", "api").Logger()
	srv := api.NewHTTPServer(logger, rpc.DefaultHTTPTimeouts)
	supportedAPIs := api.SupportedAPIs(config, store)

	srv.EnableRPC(supportedAPIs)
	srv.EnableWS(supportedAPIs)

	srv.SetListenAddr("localhost", 8545)

	err := srv.Start()
	if err != nil {
		panic(err)
	}
	logger.Info().Msgf("Server Started: %s", srv.ListenAddr())
}
