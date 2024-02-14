package cmd

import (
	"context"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/events"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/rs/zerolog"
	"os"
	"os/signal"
	"syscall"
)

func StartIngestion(cfg *config.Config) error {

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	logger.Info().Msg("starting up the EVM gateway")

	pebbleDB, err := pebble.New(cfg.DatabaseDir, logger)
	if err != nil {
		return err
	}

	opts := make([]pebble.BlockOption, 0)
	// if initialization height is provided use that to bootstrap the database
	if cfg.InitHeight != config.EmptyHeight {
		opts = append(opts, pebble.WithInitHeight(cfg.InitHeight))
	}

	blocks, err := pebble.NewBlocks(pebbleDB, opts...)
	if err != nil {
		return err
	}
	transactions := pebble.NewTransactions(pebbleDB)
	receipts := pebble.NewReceipts(pebbleDB)

	latest, err := blocks.LatestHeight()
	if err != nil {
		return err
	}

	first, err := blocks.FirstHeight()
	if err != nil {
		return err
	}

	logger.Info().Uint64("first", first).Uint64("latest", latest).Msg("index already contains data")

	client, err := grpc.NewClient(cfg.AccessNodeGRPCHost)
	if err != nil {
		return err
	}

	blk, err := client.GetLatestBlock(context.Background(), false)
	if err != nil {
		return err
	}

	logger.Info().Uint64("cadence height", blk.Height).Msg("latest flow block on the network")

	subscriber := events.NewRPCSubscriber(client)
	engine := events.NewEventIngestionEngine(subscriber, blocks, receipts, transactions, logger)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err = engine.Start(ctx)
		if err != nil {
			logger.Error().Err(err)
			panic(err)
		}
	}()

	<-engine.Ready() // wait for engine to be ready

	logger.Info().Msg("EVM gateway start up successful")

	gracefulShutdown := make(chan os.Signal, 1)
	signal.Notify(gracefulShutdown, syscall.SIGINT, syscall.SIGTERM)

	<-gracefulShutdown
	logger.Info().Msg("shutdown signal received, shutting down")
	cancel()

	return nil
}

func StartServer(cfg *config.Config, logger zerolog.Logger) error {
	srv := api.NewHTTPServer(logger, rpc.DefaultHTTPTimeouts)

	flowClient, err := api.NewFlowClient(grpc.EmulatorHost)
	if err != nil {
		return err
	}

	store := storage.NewStore()
	blockchainAPI := api.NewBlockChainAPI(cfg, store, flowClient)
	supportedAPIs := api.SupportedAPIs(blockchainAPI)

	if err := srv.EnableRPC(supportedAPIs); err != nil {
		return err
	}

	if err := srv.EnableWS(supportedAPIs); err != nil {
		return err
	}

	if err := srv.SetListenAddr("", 8545); err != nil {
		return err
	}

	if err := srv.Start(); err != nil {
		return err
	}

	logger.Info().Msgf("Server Started: %s", srv.ListenAddr())

	return nil
}
