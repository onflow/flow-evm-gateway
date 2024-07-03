package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	broadcast "github.com/onflow/flow-go/engine"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter/memorystore"

	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/services/traces"
	"github.com/onflow/flow-evm-gateway/storage"
	storageErrs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

func Start(ctx context.Context, cfg *config.Config) error {
	logger := zerolog.New(cfg.LogWriter).With().Timestamp().Logger()
	logger = logger.Level(cfg.LogLevel)
	logger.Info().Msg("starting up the EVM gateway")

	pebbleDB, err := pebble.New(cfg.DatabaseDir, logger)
	if err != nil {
		return err
	}

	blocks := pebble.NewBlocks(pebbleDB)
	transactions := pebble.NewTransactions(pebbleDB)
	receipts := pebble.NewReceipts(pebbleDB)
	accounts := pebble.NewAccounts(pebbleDB)
	trace := pebble.NewTraces(pebbleDB)

	blocksBroadcaster := broadcast.NewBroadcaster()
	transactionsBroadcaster := broadcast.NewBroadcaster()
	logsBroadcaster := broadcast.NewBroadcaster()

	// this should only be used locally or for testing
	if cfg.ForceStartCadenceHeight != 0 {
		logger.Warn().Uint64("height", cfg.ForceStartCadenceHeight).Msg("force setting starting Cadence height!!!")
		if err := blocks.SetLatestCadenceHeight(cfg.ForceStartCadenceHeight); err != nil {
			return err
		}
	}

	// create access client with cross-spork capabilities
	currentSporkClient, err := grpc.NewClient(cfg.AccessNodeHost)
	if err != nil {
		return fmt.Errorf("failed to create client connection for host: %s, with error: %w", cfg.AccessNodeHost, err)
	}

	// if we provided access node previous spork hosts add them to the client
	pastSporkClients := make([]access.Client, len(cfg.AccessNodePreviousSporkHosts))
	for i, host := range cfg.AccessNodePreviousSporkHosts {
		grpcClient, err := grpc.NewClient(host)
		if err != nil {
			return fmt.Errorf("failed to create client connection for host: %s, with error: %w", host, err)
		}

		pastSporkClients[i] = grpcClient
	}

	client, err := requester.NewCrossSporkClient(
		currentSporkClient,
		pastSporkClients,
		logger,
		cfg.FlowNetworkID,
	)
	if err != nil {
		return err
	}

	// if database is not initialized require init height
	if _, err := blocks.LatestCadenceHeight(); errors.Is(err, storageErrs.ErrNotInitialized) {
		cadenceHeight := cfg.InitCadenceHeight
		cadenceBlock, err := client.GetBlockHeaderByHeight(ctx, cadenceHeight)
		if err != nil {
			return err
		}

		if err := blocks.InitHeights(cadenceHeight, cadenceBlock.ID); err != nil {
			return fmt.Errorf("failed to init the database: %w", err)
		}
		logger.Info().Msg("database initialized with 0 evm and cadence heights")
	}

	go func() {
		err := startServer(
			ctx,
			cfg,
			client,
			blocks,
			transactions,
			receipts,
			accounts,
			trace,
			blocksBroadcaster,
			transactionsBroadcaster,
			logsBroadcaster,
			logger,
		)
		if err != nil {
			logger.Error().Err(err).Msg("failed to start the API server")
			panic(err)
		}
	}()

	err = startIngestion(
		ctx,
		cfg,
		client,
		blocks,
		transactions,
		receipts,
		accounts,
		trace,
		blocksBroadcaster,
		transactionsBroadcaster,
		logsBroadcaster,
		logger,
	)
	if err != nil {
		return fmt.Errorf("failed to start event ingestion: %w", err)
	}

	return nil
}

func startIngestion(
	ctx context.Context,
	cfg *config.Config,
	client *requester.CrossSporkClient,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	trace storage.TraceIndexer,
	blocksBroadcaster *broadcast.Broadcaster,
	transactionsBroadcaster *broadcast.Broadcaster,
	logsBroadcaster *broadcast.Broadcaster,
	logger zerolog.Logger,
) error {
	logger.Info().Msg("starting up event ingestion")

	blk, err := client.GetLatestBlock(context.Background(), false)
	if err != nil {
		return fmt.Errorf("failed to get latest cadence block: %w", err)
	}

	latestCadenceHeight, err := blocks.LatestCadenceHeight()
	if err != nil {
		return err
	}

	// make sure the provided block to start the indexing can be loaded
	_, err = client.GetBlockHeaderByHeight(context.Background(), latestCadenceHeight)
	if err != nil {
		return fmt.Errorf("failed to get provided cadence height: %w", err)
	}

	logger.Info().
		Uint64("start-cadence-height", latestCadenceHeight).
		Uint64("latest-cadence-height", blk.Height).
		Uint64("missed-heights", blk.Height-latestCadenceHeight).
		Msg("indexing cadence height information")

	subscriber := ingestion.NewRPCSubscriber(
		client,
		cfg.HeartbeatInterval,
		cfg.FlowNetworkID,
		logger,
	)

	if cfg.TracesEnabled {
		downloader, err := traces.NewGCPDownloader(cfg.TracesBucketName, logger)
		if err != nil {
			return err
		}

		initHeight, err := blocks.LatestEVMHeight()
		if err != nil {
			return err
		}
		tracesEngine := traces.NewTracesIngestionEngine(
			initHeight,
			blocksBroadcaster,
			blocks,
			trace,
			downloader,
			logger,
		)

		go func() {
			err = tracesEngine.Run(ctx)
			if err != nil {
				logger.Error().Err(err).Msg("traces ingestion engine failed to run")
				panic(err)
			}
		}()

		<-tracesEngine.Ready()
	}

	eventEngine := ingestion.NewEventIngestionEngine(
		subscriber,
		blocks,
		receipts,
		transactions,
		accounts,
		blocksBroadcaster,
		transactionsBroadcaster,
		logsBroadcaster,
		logger,
	)
	const retries = 15
	restartableEventEngine := models.NewRestartableEngine(eventEngine, retries, logger)

	go func() {
		err = restartableEventEngine.Run(ctx)
		if err != nil {
			logger.Error().Err(err).Msg("event ingestion engine failed to run")
			panic(err)
		}
	}()

	// wait for ingestion engines to be ready
	<-restartableEventEngine.Ready()

	logger.Info().Msg("ingestion start up successful")
	return nil
}

func startServer(
	ctx context.Context,
	cfg *config.Config,
	client *requester.CrossSporkClient,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	trace storage.TraceIndexer,
	blocksBroadcaster *broadcast.Broadcaster,
	transactionsBroadcaster *broadcast.Broadcaster,
	logsBroadcaster *broadcast.Broadcaster,
	logger zerolog.Logger,
) error {
	l := logger.With().Str("component", "API").Logger()
	l.Info().Msg("starting up RPC server")

	srv := api.NewHTTPServer(l, cfg)

	// create the signer based on either a single coa key being provided and using a simple in-memory
	// signer, or multiple keys being provided and using signer with key-rotation mechanism.
	var signer crypto.Signer
	var err error
	switch {
	case cfg.COAKey != nil:
		signer, err = crypto.NewInMemorySigner(cfg.COAKey, crypto.SHA3_256)
	case cfg.COAKeys != nil:
		signer, err = requester.NewKeyRotationSigner(cfg.COAKeys, crypto.SHA3_256)
	case len(cfg.COACloudKMSKeys) > 0:
		signer, err = requester.NewKMSKeyRotationSigner(
			ctx,
			cfg.COACloudKMSKeys,
			logger,
		)
	default:
		return fmt.Errorf("must either provide single COA key, or list of COA keys")
	}
	if err != nil {
		return fmt.Errorf("failed to create a COA signer: %w", err)
	}

	evm, err := requester.NewEVM(
		client,
		cfg,
		signer,
		logger,
		blocks,
	)
	if err != nil {
		return fmt.Errorf("failed to create EVM requester: %w", err)
	}

	// create rate limiter for requests on the APIs. Tokens are number of requests allowed per 1 second interval
	// if no limit is defined we specify max value, effectively disabling rate-limiting
	rateLimit := cfg.RateLimit
	if rateLimit == 0 {
		l.Warn().Msg("no rate-limiting is set")
		rateLimit = math.MaxInt
	}
	ratelimiter, err := memorystore.New(&memorystore.Config{Tokens: rateLimit, Interval: time.Second})
	if err != nil {
		return fmt.Errorf("failed to create rate limiter: %w", err)
	}

	blockchainAPI, err := api.NewBlockChainAPI(
		logger,
		cfg,
		evm,
		blocks,
		transactions,
		receipts,
		accounts,
		ratelimiter,
	)
	if err != nil {
		return err
	}

	streamAPI := api.NewStreamAPI(
		logger,
		cfg,
		blocks,
		transactions,
		receipts,
		blocksBroadcaster,
		transactionsBroadcaster,
		logsBroadcaster,
		ratelimiter,
	)

	pullAPI := api.NewPullAPI(
		logger,
		cfg,
		blocks,
		transactions,
		receipts,
		ratelimiter,
	)

	var debugAPI *api.DebugAPI
	if cfg.TracesEnabled {
		debugAPI = api.NewDebugAPI(trace, blocks, logger)
	}

	var walletAPI *api.WalletAPI
	if cfg.WalletEnabled {
		walletAPI = api.NewWalletAPI(cfg, blockchainAPI)
	}

	supportedAPIs := api.SupportedAPIs(
		blockchainAPI,
		streamAPI,
		pullAPI,
		debugAPI,
		walletAPI,
		cfg,
	)

	if err := srv.EnableRPC(supportedAPIs); err != nil {
		return err
	}

	if cfg.WSEnabled {
		if err := srv.EnableWS(supportedAPIs); err != nil {
			return err
		}
	}

	if err := srv.SetListenAddr(cfg.RPCHost, cfg.RPCPort); err != nil {
		return err
	}

	if err := srv.Start(); err != nil {
		return err
	}

	logger.Info().Msgf("server Started: %s", srv.ListenAddr())

	<-ctx.Done()
	logger.Info().Msg("shutting down API server")
	srv.Stop()

	return nil
}
