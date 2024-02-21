package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
	storageErrs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/rs/zerolog"
	"os"
	"os/signal"
	"syscall"
)

func Start(cfg *config.Config) error {
	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	logger.Info().Msg("starting up the EVM gateway")

	pebbleDB, err := pebble.New(cfg.DatabaseDir, logger)
	if err != nil {
		return err
	}

	blocks := pebble.NewBlocks(pebbleDB)
	transactions := pebble.NewTransactions(pebbleDB)
	receipts := pebble.NewReceipts(pebbleDB)
	accounts := pebble.NewAccounts(pebbleDB)

	// if initialization height is provided use that to bootstrap the database, but only if it's empty
	if cfg.InitHeight != config.EmptyHeight {
		logger.Info().
			Uint64("init height", cfg.InitHeight).
			Msg("initializing the indexer with init height")

		val, err := blocks.LatestCadenceHeight()
		if errors.Is(err, storageErrs.NotInitialized) { // if database is empty we allow init
			err = blocks.InitHeight(cfg.InitHeight)
			if err != nil {
				return fmt.Errorf("failed to init height: %w", err)
			}
		} else { // otherwise we ignore init height and warn
			logger.Warn().
				Uint64("current latest height", val).
				Msg("setting init height value no an already initialized database is not possible")
		}
	}

	fmt.Println("#here 4")

	go func() {
		err := startServer(cfg, blocks, transactions, receipts, accounts, logger)
		if err != nil {
			logger.Fatal().Err(fmt.Errorf("failed to start RPC server: %w", err))
		}
	}()

	err = startIngestion(cfg, blocks, transactions, receipts, accounts, logger)
	if err != nil {
		return fmt.Errorf("failed to start event ingestion: %w", err)
	}

	return nil
}

func startIngestion(
	cfg *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	logger zerolog.Logger,
) error {
	logger.Info().Msg("starting up event ingestion")

	latest, err := blocks.LatestEVMHeight()
	if err != nil {
		return err
	}

	first, err := blocks.FirstEVMHeight()
	if err != nil {
		return err
	}

	latestCadence, err := blocks.LatestCadenceHeight()
	if err != nil {
		return err
	}

	client, err := grpc.NewClient(cfg.AccessNodeGRPCHost)
	if err != nil {
		return err
	}

	blk, err := client.GetLatestBlock(context.Background(), false)
	if err != nil {
		return err
	}

	logger.Info().
		Uint64("first evm", first).
		Uint64("latest evm", latest).
		Uint64("latest cadence", latestCadence).
		Uint64("cadence height", blk.Height).
		Uint64("cadence heights to index", blk.Height-latestCadence).
		Msg("indexing block heights status")

	subscriber := ingestion.NewRPCSubscriber(client)
	engine := ingestion.NewEventIngestionEngine(
		subscriber,
		blocks,
		receipts,
		transactions,
		accounts,
		logger,
	)

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

func startServer(
	cfg *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	logger zerolog.Logger,
) error {
	logger.Info().Msg("starting up RPC server")

	srv := api.NewHTTPServer(logger, rpc.DefaultHTTPTimeouts)

	client, err := grpc.NewClient(cfg.AccessNodeGRPCHost)
	if err != nil {
		return err
	}

	// todo abstract signer and support multiple different signers, for now only in-memory
	signer, err := crypto.NewInMemorySigner(cfg.COAKey, crypto.SHA3_256)
	if err != nil {
		return fmt.Errorf("failed to create a COA signer: %w", err)
	}

	evm, err := requester.NewEVM(client, cfg.COAAddress, signer, true, logger)
	if err != nil {
		return fmt.Errorf("failed to create EVM requester: %w", err)
	}

	blockchainAPI := api.NewBlockChainAPI(
		logger,
		cfg,
		evm,
		blocks,
		transactions,
		receipts,
		accounts,
	)
	supportedAPIs := api.SupportedAPIs(blockchainAPI)

	if err := srv.EnableRPC(supportedAPIs); err != nil {
		return err
	}

	if err := srv.EnableWS(supportedAPIs); err != nil {
		return err
	}

	if err := srv.SetListenAddr(cfg.RPCHost, cfg.RPCPort); err != nil {
		return err
	}

	if err := srv.Start(); err != nil {
		return err
	}

	logger.Info().Msgf("Server Started: %s", srv.ListenAddr())

	return nil
}
