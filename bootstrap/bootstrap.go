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
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter/memorystore"
	grpcOpts "google.golang.org/grpc"

	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/services/state"
	"github.com/onflow/flow-evm-gateway/services/traces"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

type Storages struct {
	Storage      *pebble.Storage
	Blocks       storage.BlockIndexer
	Transactions storage.TransactionIndexer
	Receipts     storage.ReceiptIndexer
	Accounts     storage.AccountIndexer
	Traces       storage.TraceIndexer
	Registers    *pebble.Register
}

type Publishers struct {
	Block       *models.Publisher
	Transaction *models.Publisher
	Logs        *models.Publisher
}

type Bootstrap struct {
	logger     zerolog.Logger
	config     *config.Config
	Client     *requester.CrossSporkClient
	EVMClient  requester.EVMClient
	Storages   *Storages
	Publishers *Publishers
	collector  metrics.Collector
	Server     *api.Server
	metrics    *metrics.Server
	Events     *ingestion.Engine
	Traces     *traces.Engine
	State      *state.Engine
}

func New(config *config.Config) (*Bootstrap, error) {
	logger := zerolog.New(config.LogWriter).
		With().Timestamp().Logger().Level(config.LogLevel)

	client, err := setupCrossSporkClient(config, logger)
	if err != nil {
		return nil, err
	}

	storages, err := setupStorage(config, client, logger)
	if err != nil {
		return nil, err
	}

	return &Bootstrap{
		Publishers: &Publishers{
			Block:       models.NewPublisher(),
			Transaction: models.NewPublisher(),
			Logs:        models.NewPublisher(),
		},
		Storages:  storages,
		logger:    logger,
		config:    config,
		Client:    client,
		collector: metrics.NewCollector(logger),
	}, nil
}

func (b *Bootstrap) StartEventIngestion(ctx context.Context) error {
	l := b.logger.With().Str("component", "bootstrap-ingestion").Logger()
	l.Info().Msg("bootstrap starting event ingestion")

	// get latest cadence block from the network and the database
	latestCadenceBlock, err := b.Client.GetLatestBlock(context.Background(), true)
	if err != nil {
		return fmt.Errorf("failed to get latest cadence block: %w", err)
	}

	latestCadenceHeight, err := b.Storages.Blocks.LatestCadenceHeight()
	if err != nil {
		return err
	}

	// make sure the provided block to start the indexing can be loaded
	_, err = b.Client.GetBlockHeaderByHeight(context.Background(), latestCadenceHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to get provided cadence height %d: %w",
			latestCadenceHeight,
			err,
		)
	}

	l.Info().
		Uint64("start-cadence-height", latestCadenceHeight).
		Uint64("latest-cadence-height", latestCadenceBlock.Height).
		Uint64("missed-heights", latestCadenceBlock.Height-latestCadenceHeight).
		Msg("indexing cadence height information")

	// create event subscriber
	subscriber := ingestion.NewRPCSubscriber(
		b.Client,
		b.config.HeartbeatInterval,
		b.config.FlowNetworkID,
		b.logger,
	)

	// initialize event ingestion engine
	b.Events = ingestion.NewEventIngestionEngine(
		subscriber,
		b.Storages.Storage,
		b.Storages.Blocks,
		b.Storages.Receipts,
		b.Storages.Transactions,
		b.Storages.Accounts,
		b.Publishers.Block,
		b.Publishers.Logs,
		b.logger,
		b.collector,
	)

	startEngine(ctx, b.Events, l)
	return nil
}

func (b *Bootstrap) StartTraceDownloader(ctx context.Context) error {
	l := b.logger.With().Str("component", "bootstrap-traces").Logger()
	l.Info().Msg("starting engine")

	// create gcp downloader
	downloader, err := traces.NewGCPDownloader(b.config.TracesBucketName, b.logger)
	if err != nil {
		return err
	}

	// initialize trace downloader engine
	b.Traces = traces.NewTracesIngestionEngine(
		b.Publishers.Block,
		b.Storages.Blocks,
		b.Storages.Traces,
		downloader,
		b.logger,
		b.collector,
	)

	startEngine(ctx, b.Traces, l)
	return nil
}

func (b *Bootstrap) StopTraceDownloader() {
	if b.Traces == nil {
		return
	}
	b.logger.Warn().Msg("stopping trace downloader engine")
	b.Traces.Stop()
}

func (b *Bootstrap) StopEventIngestion() {
	if b.Events == nil {
		return
	}
	b.logger.Warn().Msg("stopping event ingestion engine")
	b.Events.Stop()
}

func (b *Bootstrap) StartStateIndex(ctx context.Context) error {
	l := b.logger.With().Str("component", "bootstrap-state").Logger()
	l.Info().Msg("starting engine")

	execution, ok := b.Client.Client.(*grpc.Client)
	if !ok {
		return fmt.Errorf("execution data client not supported on the provided AN client")
	}

	b.State = state.NewStateEngine(
		b.config,
		execution.ExecutionDataRPCClient(),
		b.Publishers.Block,
		b.Storages.Storage,
		b.Storages.Blocks,
		b.Storages.Transactions,
		b.Storages.Receipts,
		b.logger,
	)

	startEngine(ctx, b.State, l)
	return nil
}

func (b *Bootstrap) StopStateIndex() {
	if b.State == nil {
		return
	}
	b.logger.Warn().Msg("stopping state index engine")
	b.State.Stop()
}

func (b *Bootstrap) StartAPIServer(ctx context.Context) error {
	b.logger.Info().Msg("bootstrap starting metrics server")

	b.Server = api.NewServer(b.logger, b.collector, b.config)

	// create the signer based on either a single coa key being provided and using a simple in-memory
	// signer, or multiple keys being provided and using signer with key-rotation mechanism.
	var signer crypto.Signer
	var err error
	switch {
	case b.config.COAKey != nil:
		signer, err = crypto.NewInMemorySigner(b.config.COAKey, crypto.SHA3_256)
	case b.config.COAKeys != nil:
		signer, err = requester.NewKeyRotationSigner(b.config.COAKeys, crypto.SHA3_256)
	case len(b.config.COACloudKMSKeys) > 0:
		signer, err = requester.NewKMSKeyRotationSigner(
			ctx,
			b.config.COACloudKMSKeys,
			b.logger,
		)
	default:
		return fmt.Errorf("must provide either single COA / keylist of COA keys / COA cloud KMS keys")
	}
	if err != nil {
		return fmt.Errorf("failed to create a COA signer: %w", err)
	}

	// create transaction pool
	txPool := requester.NewTxPool(b.Client, b.Publishers.Transaction, b.logger)

	b.EVMClient, err = requester.NewClientHandler(
		b.config,
		b.Storages.Storage,
		txPool,
		signer,
		b.Client,
		b.Storages.Blocks,
		b.Storages.Receipts,
		b.logger,
		b.collector,
	)
	if err != nil {
		return fmt.Errorf("failed to create EVM requester: %w", err)
	}

	// create rate limiter for requests on the APIs. Tokens are number of requests allowed per 1 second interval
	// if no limit is defined we specify max value, effectively disabling rate-limiting
	rateLimit := b.config.RateLimit
	if rateLimit == 0 {
		b.logger.Warn().Msg("no rate-limiting is set")
		rateLimit = math.MaxInt
	}
	ratelimiter, err := memorystore.New(&memorystore.Config{Tokens: rateLimit, Interval: time.Second})
	if err != nil {
		return fmt.Errorf("failed to create rate limiter: %w", err)
	}

	blockchainAPI, err := api.NewBlockChainAPI(
		b.logger,
		b.config,
		b.EVMClient,
		b.Storages.Blocks,
		b.Storages.Transactions,
		b.Storages.Receipts,
		b.Storages.Accounts,
		ratelimiter,
		b.collector,
	)
	if err != nil {
		return err
	}

	streamAPI := api.NewStreamAPI(
		b.logger,
		b.config,
		b.Storages.Blocks,
		b.Storages.Transactions,
		b.Storages.Receipts,
		b.Publishers.Block,
		b.Publishers.Transaction,
		b.Publishers.Logs,
		ratelimiter,
	)

	pullAPI := api.NewPullAPI(
		b.logger,
		b.config,
		b.Storages.Blocks,
		b.Storages.Transactions,
		b.Storages.Receipts,
		ratelimiter,
	)

	var debugAPI *api.DebugAPI
	if b.config.TracesEnabled {
		debugAPI = api.NewDebugAPI(b.Storages.Traces, b.Storages.Blocks, b.logger, b.collector)
	}

	var walletAPI *api.WalletAPI
	if b.config.WalletEnabled {
		walletAPI = api.NewWalletAPI(b.config, blockchainAPI)
	}

	supportedAPIs := api.SupportedAPIs(
		blockchainAPI,
		streamAPI,
		pullAPI,
		debugAPI,
		walletAPI,
		b.config,
	)

	if err := b.Server.EnableRPC(supportedAPIs); err != nil {
		return err
	}

	if b.config.WSEnabled {
		if err := b.Server.EnableWS(supportedAPIs); err != nil {
			return err
		}
	}

	if err := b.Server.SetListenAddr(b.config.RPCHost, b.config.RPCPort); err != nil {
		return err
	}

	if err := b.Server.Start(); err != nil {
		return err
	}

	b.logger.Info().Msgf("API server started: %s", b.Server.ListenAddr())
	return nil
}

func (b *Bootstrap) StopAPIServer() {
	if b.Server == nil {
		return
	}
	b.logger.Warn().Msg("shutting down API server")
	b.Server.Stop()
}

func (b *Bootstrap) StartMetricsServer(_ context.Context) error {
	b.logger.Info().Msg("bootstrap starting metrics server")

	b.metrics = metrics.NewServer(b.logger, b.config.MetricsPort)
	started, err := b.metrics.Start()
	if err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}
	<-started

	return nil
}

func (b *Bootstrap) StopMetricsServer() {
	if b.metrics == nil {
		return
	}
	b.logger.Warn().Msg("shutting down metrics server")
	b.metrics.Stop()
}

// startEngine starts provided engine and panics if there are startup errors.
func startEngine(
	ctx context.Context,
	engine models.Engine,
	logger zerolog.Logger,
) {
	logger.Info().Msg("starting engine")
	go func() {
		err := engine.Run(ctx)
		if err != nil {
			logger.Error().Err(err).Msg("engine failed to run")
			panic(err)
		}
	}()

	<-engine.Ready()
	logger.Info().Msg("engine started successfully")
}

// setupCrossSporkClient sets up a cross-spork AN client.
func setupCrossSporkClient(config *config.Config, logger zerolog.Logger) (*requester.CrossSporkClient, error) {
	// create access client with cross-spork capabilities
	currentSporkClient, err := grpc.NewClient(
		config.AccessNodeHost,
		grpc.WithGRPCDialOptions(grpcOpts.WithDefaultCallOptions(grpcOpts.MaxCallRecvMsgSize(1024*1024*1024))),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create client connection for host: %s, with error: %w",
			config.AccessNodeHost,
			err,
		)
	}

	// if we provided access node previous spork hosts add them to the client
	pastSporkClients := make([]access.Client, len(config.AccessNodePreviousSporkHosts))
	for i, host := range config.AccessNodePreviousSporkHosts {
		grpcClient, err := grpc.NewClient(host)
		if err != nil {
			return nil, fmt.Errorf("failed to create client connection for host: %s, with error: %w", host, err)
		}

		pastSporkClients[i] = grpcClient
	}

	// initialize cross spork client to the access nodes
	client, err := requester.NewCrossSporkClient(
		currentSporkClient,
		pastSporkClients,
		logger,
		config.FlowNetworkID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cross spork client: %w", err)
	}

	return client, nil
}

// setupStorage creates storage and initializes it with configured starting cadence height
// in case such a height doesn't already exist in the database.
func setupStorage(
	config *config.Config,
	client *requester.CrossSporkClient,
	logger zerolog.Logger,
) (*Storages, error) {
	// create pebble storage from the provided database root directory
	store, err := pebble.New(config.DatabaseDir, logger)
	if err != nil {
		return nil, err
	}

	blocks := pebble.NewBlocks(store, config.FlowNetworkID)

	// hard set the start cadence height, this is used when force reindexing
	if config.ForceStartCadenceHeight != 0 {
		logger.Warn().Uint64("height", config.ForceStartCadenceHeight).Msg("force setting starting Cadence height!!!")
		if err := blocks.SetLatestCadenceHeight(config.ForceStartCadenceHeight, nil); err != nil {
			return nil, err
		}
	}

	// if database is not initialized require init height
	if _, err := blocks.LatestCadenceHeight(); errors.Is(err, errs.ErrStorageNotInitialized) {
		cadenceHeight := config.InitCadenceHeight
		cadenceBlock, err := client.GetBlockHeaderByHeight(context.Background(), cadenceHeight)
		if err != nil {
			return nil, fmt.Errorf("could not fetch provided cadence height, make sure it's correct: %w", err)
		}

		if err := blocks.InitHeights(cadenceHeight, cadenceBlock.ID); err != nil {
			return nil, fmt.Errorf(
				"failed to init the database for block height: %d and ID: %s, with : %w",
				cadenceHeight,
				cadenceBlock.ID,
				err,
			)
		}
		logger.Info().Msgf("database initialized with cadence height: %d", cadenceHeight)
	}

	return &Storages{
		Storage:      store,
		Blocks:       blocks,
		Transactions: pebble.NewTransactions(store),
		Receipts:     pebble.NewReceipts(store),
		Accounts:     pebble.NewAccounts(store),
		Traces:       pebble.NewTraces(store),
	}, nil
}

// Run will run complete bootstrap of the EVM gateway with all the engines.
// Run is a blocking call, but it does signal readiness of the service
// through a channel provided as an argument.
func Run(ctx context.Context, cfg *config.Config, ready chan struct{}) error {
	boot, err := New(cfg)
	if err != nil {
		return err
	}

	if cfg.TracesEnabled {
		if err := boot.StartTraceDownloader(ctx); err != nil {
			return fmt.Errorf("failed to start trace downloader engine: %w", err)
		}
	}

	if err := boot.StartStateIndex(ctx); err != nil {
		return fmt.Errorf("failed to start local state index engine: %w", err)
	}

	// we must wait for state index to be ready before starting the ingestion engine,
	// because state index might have to catch-up executed blocks to indexed block height
	<-boot.State.Ready()

	if err := boot.StartEventIngestion(ctx); err != nil {
		return fmt.Errorf("failed to start event ingestion engine: %w", err)
	}

	if err := boot.StartAPIServer(ctx); err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}

	if err := boot.StartMetricsServer(ctx); err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}

	// wait for event ingestion engine
	<-boot.Events.Ready()

	// mark ready
	close(ready)

	// if context is canceled start shutdown
	<-ctx.Done()
	boot.logger.Warn().Msg("bootstrap received context cancellation, stopping services")

	boot.StopEventIngestion()
	boot.StopMetricsServer()
	boot.StopTraceDownloader()
	boot.StopAPIServer()

	return nil
}
