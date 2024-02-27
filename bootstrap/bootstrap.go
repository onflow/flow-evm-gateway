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
)

func Start(ctx context.Context, cfg *config.Config) error {
	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	logger.Level(zerolog.DebugLevel) // todo use cfg flag
	logger.Info().Msg("starting up the EVM gateway")

	pebbleDB, err := pebble.New(cfg.DatabaseDir, logger)
	if err != nil {
		return err
	}

	blocks := pebble.NewBlocks(pebbleDB)
	transactions := pebble.NewTransactions(pebbleDB)
	receipts := pebble.NewReceipts(pebbleDB)
	accounts := pebble.NewAccounts(pebbleDB)

	// if database is not initialized require init height
	if _, err := blocks.LatestCadenceHeight(); errors.Is(err, storageErrs.ErrNotInitialized) {
		if cfg.InitCadenceHeight == config.EmptyHeight {
			return fmt.Errorf("must provide init cadence height on an empty database")
		}
		logger.Info().
			Uint64("cadence-height", cfg.InitCadenceHeight).
			Msg("database cadence height init")

		if err := blocks.InitCadenceHeight(cfg.InitCadenceHeight); err != nil {
			return err
		}
	}

	go func() {
		err := startServer(ctx, cfg, blocks, transactions, receipts, accounts, logger)
		if err != nil {
			logger.Error().Err(err).Msg("failed to start the API server")
			panic(err)
		}
	}()

	err = startIngestion(ctx, cfg, blocks, transactions, receipts, accounts, logger)
	if err != nil {
		return fmt.Errorf("failed to start event ingestion: %w", err)
	}

	return nil
}

func startIngestion(
	ctx context.Context,
	cfg *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	logger zerolog.Logger,
) error {
	logger.Info().Msg("starting up event ingestion")

	client, err := grpc.NewClient(cfg.AccessNodeGRPCHost)
	if err != nil {
		return err
	}

	blk, err := client.GetLatestBlock(context.Background(), false)
	if err != nil {
		return fmt.Errorf("failed to get latest cadence block: %w", err)
	}

	latestCadenceHeight, err := blocks.LatestCadenceHeight()
	if err != nil {
		return err
	}

	// make sure the provided block to start the indexing can be loaded
	_, err = client.GetBlockByHeight(context.Background(), latestCadenceHeight)
	if err != nil {
		return fmt.Errorf("failed to get provided cadence height: %w", err)
	}

	logger.Info().
		Uint64("start-height", latestCadenceHeight).
		Uint64("latest-network-height", blk.Height).
		Uint64("missed-heights", blk.Height-latestCadenceHeight).
		Msg("indexing cadence height information")

	subscriber := ingestion.NewRPCSubscriber(client)
	engine := ingestion.NewEventIngestionEngine(
		subscriber,
		blocks,
		receipts,
		transactions,
		accounts,
		logger,
	)

	go func() {
		err = engine.Run(ctx)
		if err != nil {
			logger.Error().Err(err).Msg("failed to start ingestion engine")
			panic(err)
		}
	}()

	<-engine.Ready() // wait for engine to be ready

	logger.Info().Msg("Ingestion start up successful")
	return nil
}

func startServer(
	ctx context.Context,
	cfg *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	logger zerolog.Logger,
) error {
	l := logger.With().Str("component", "API").Logger()
	l.Info().Msg("starting up RPC server")

	srv := api.NewHTTPServer(l, rpc.DefaultHTTPTimeouts)

	client, err := grpc.NewClient(cfg.AccessNodeGRPCHost)
	if err != nil {
		return err
	}

	// create the signer based on either a single coa key being provided and using a simple in-memory
	// signer, or multiple keys being provided and using signer with key-rotation mechanism.
	var signer crypto.Signer
	switch {
	case cfg.COAKey != nil:
		signer, err = crypto.NewInMemorySigner(cfg.COAKey, crypto.SHA3_256)
	case cfg.COAKeys != nil:
		signer, err = requester.NewKeyRotationSigner(cfg.COAKeys, crypto.SHA3_256)
	default:
		return fmt.Errorf("must either provide single COA key, or list of COA keys")
	}
	if err != nil {
		return fmt.Errorf("failed to create a COA signer: %w", err)
	}

	evm, err := requester.NewEVM(client, cfg.COAAddress, signer, cfg.FlowNetworkID, true, logger)
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

	<-ctx.Done()
	logger.Info().Msg("Shutting down API server")
	srv.Stop()

	return nil
}
