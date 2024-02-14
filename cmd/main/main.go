package main

import (
	"github.com/onflow/flow-evm-gateway/cmd"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	cfg, err := config.FromFlags()
	if err != nil {
		logger.Fatal().Err(err)
	}

	go func() {
		err := cmd.StartServer(cfg, logger)
		if err != nil {
			logger.Fatal().Err(err)
		}
	}()

	err = cmd.StartIngestion(cfg)
	if err != nil {
		logger.Fatal().Err(err)
	}
}
