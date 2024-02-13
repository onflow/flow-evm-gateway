package main

import (
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	cfg, err := config.FromFlags()
	if err != nil {
		logger.Fatal().Err(err)
	}

	// start server
	go func() {
		runServer(cfg, logger)
	}()

	// start ingestion
	err = start(cfg)
	if err != nil {
		logger.Fatal().Err(err)
	}
}

func runServer(cfg *config.Config, logger zerolog.Logger) {
	store := storage.NewStore()

	logger = logger.With().Str("component", "api").Logger()
	srv := api.NewHTTPServer(logger, rpc.DefaultHTTPTimeouts)
	supportedAPIs := api.SupportedAPIs(cfg, store)

	srv.EnableRPC(supportedAPIs)
	srv.EnableWS(supportedAPIs)

	srv.SetListenAddr("localhost", 8545)

	err := srv.Start()
	if err != nil {
		panic(err)
	}
	logger.Info().Msgf("Server Started: %s", srv.ListenAddr())
}
