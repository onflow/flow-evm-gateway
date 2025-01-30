package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	pebbleDB "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/environment"
	"github.com/onflow/flow-go/fvm/evm"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/module/component"
	flowMetrics "github.com/onflow/flow-go/module/metrics"
	"github.com/onflow/flow-go/module/util"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter/memorystore"
	grpcOpts "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

const (
	// DefaultMaxMessageSize is the default maximum message size for gRPC responses
	DefaultMaxMessageSize = 1024 * 1024 * 1024

	// DefaultResourceExhaustedRetryDelay is the default delay between retries when the server returns
	// a ResourceExhausted error.
	DefaultResourceExhaustedRetryDelay = 100 * time.Millisecond

	// DefaultResourceExhaustedMaxRetryDelay is the default max request duration when retrying server
	// ResourceExhausted errors.
	DefaultResourceExhaustedMaxRetryDelay = 30 * time.Second
)

type Storages struct {
	Storage      *pebble.Storage
	Registers    *pebble.RegisterStorage
	Blocks       storage.BlockIndexer
	Transactions storage.TransactionIndexer
	Receipts     storage.ReceiptIndexer
	Traces       storage.TraceIndexer
}

type Publishers struct {
	Block       *models.Publisher[*models.Block]
	Transaction *models.Publisher[*gethTypes.Transaction]
	Logs        *models.Publisher[[]*gethTypes.Log]
}

type Bootstrap struct {
	logger     zerolog.Logger
	config     config.Config
	client     *requester.CrossSporkClient
	storages   *Storages
	publishers *Publishers
	collector  metrics.Collector
	server     *api.Server
	metrics    *flowMetrics.Server
	events     *ingestion.Engine
	profiler   *api.ProfileServer
	db         *pebbleDB.DB
	keystore   *requester.KeyStore
}

func New(config config.Config) (*Bootstrap, error) {
	logger := zerolog.New(config.LogWriter).
		With().Timestamp().Str("version", api.Version).
		Logger().Level(config.LogLevel)

	client, err := setupCrossSporkClient(config, logger)
	if err != nil {
		return nil, err
	}

	db, storages, err := setupStorage(config, client, logger)
	if err != nil {
		return nil, err
	}

	return &Bootstrap{
		publishers: &Publishers{
			Block:       models.NewPublisher[*models.Block](),
			Transaction: models.NewPublisher[*gethTypes.Transaction](),
			Logs:        models.NewPublisher[[]*gethTypes.Log](),
		},
		db:        db,
		storages:  storages,
		logger:    logger,
		config:    config,
		client:    client,
		collector: metrics.NewCollector(logger),
	}, nil
}

func (b *Bootstrap) StartEventIngestion(ctx context.Context) error {
	l := b.logger.With().Str("component", "bootstrap-ingestion").Logger()
	l.Info().Msg("bootstrap starting event ingestion")

	// get latest cadence block from the network and the database
	latestCadenceBlock, err := b.client.GetLatestBlock(context.Background(), true)
	if err != nil {
		return fmt.Errorf("failed to get latest cadence block: %w", err)
	}

	latestCadenceHeight, err := b.storages.Blocks.LatestCadenceHeight()
	if err != nil {
		return err
	}

	// make sure the provided block to start the indexing can be loaded
	_, err = b.client.GetBlockHeaderByHeight(context.Background(), latestCadenceHeight)
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

	chainID := b.config.FlowNetworkID

	// the event subscriber takes the first block to sync from the Access node, which is the block
	// after the latest cadence block
	nextCadenceHeight := latestCadenceHeight + 1

	// create event subscriber
	var subscriber ingestion.EventSubscriber
	if b.config.ExperimentalSoftFinalityEnabled {
		subscriber = ingestion.NewRPCBlockTrackingSubscriber(
			b.logger,
			b.client,
			chainID,
			b.keystore,
			nextCadenceHeight,
		)
	} else {
		subscriber = ingestion.NewRPCEventSubscriber(
			b.logger,
			b.client,
			chainID,
			b.keystore,
			nextCadenceHeight,
		)
	}

	callTracerCollector, err := replayer.NewCallTracerCollector(b.logger)
	if err != nil {
		return err
	}
	blocksProvider := replayer.NewBlocksProvider(
		b.storages.Blocks,
		chainID,
		callTracerCollector.TxTracer(),
	)
	replayerConfig := replayer.Config{
		ChainID:             chainID,
		RootAddr:            evm.StorageAccountAddress(chainID),
		CallTracerCollector: callTracerCollector,
		ValidateResults:     true,
	}

	// initialize event ingestion engine
	b.events = ingestion.NewEventIngestionEngine(
		subscriber,
		blocksProvider,
		b.storages.Storage,
		b.storages.Registers,
		b.storages.Blocks,
		b.storages.Receipts,
		b.storages.Transactions,
		b.storages.Traces,
		b.publishers.Block,
		b.publishers.Logs,
		b.logger,
		b.collector,
		replayerConfig,
	)

	StartEngine(ctx, b.events, l)
	return nil
}

func (b *Bootstrap) StopEventIngestion() {
	if b.events == nil {
		return
	}
	b.logger.Warn().Msg("stopping event ingestion engine")
	b.events.Stop()
}

func (b *Bootstrap) StartAPIServer(ctx context.Context) error {
	b.logger.Info().Msg("bootstrap starting metrics server")

	b.server = api.NewServer(b.logger, b.collector, b.config)

	// create transaction pool
	txPool := requester.NewTxPool(
		b.client,
		b.publishers.Transaction,
		b.logger,
		b.config,
	)

	accountKeys := make([]*requester.AccountKey, 0)
	if !b.config.IndexOnly {
		account, err := b.client.GetAccount(ctx, b.config.COAAddress)
		if err != nil {
			return fmt.Errorf(
				"failed to get signer info account for address: %s, with: %w",
				b.config.COAAddress,
				err,
			)
		}
		signer, err := createSigner(ctx, b.config, b.logger)
		if err != nil {
			return err
		}
		for _, key := range account.Keys {
			// Skip account keys that do not use the same Publick Key as the
			// configured crypto.Signer object.
			if !key.PublicKey.Equals(signer.PublicKey()) {
				continue
			}
			accountKeys = append(accountKeys, &requester.AccountKey{
				AccountKey: *key,
				Address:    b.config.COAAddress,
				Signer:     signer,
			})
		}
	}

	b.keystore = requester.NewKeyStore(accountKeys)

	evm, err := requester.NewEVM(
		b.storages.Registers,
		b.client,
		b.config,
		b.logger,
		b.storages.Blocks,
		txPool,
		b.collector,
		b.keystore,
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

	// get the height from which the indexing resumed since the last restart,
	// this is needed for the `eth_syncing` endpoint.
	indexingResumedHeight, err := b.storages.Blocks.LatestEVMHeight()
	if err != nil {
		return fmt.Errorf("failed to retrieve the indexing resumed height: %w", err)
	}

	blockchainAPI := api.NewBlockChainAPI(
		b.logger,
		b.config,
		evm,
		b.storages.Blocks,
		b.storages.Transactions,
		b.storages.Receipts,
		ratelimiter,
		b.collector,
		indexingResumedHeight,
	)

	streamAPI := api.NewStreamAPI(
		b.logger,
		b.config,
		b.storages.Blocks,
		b.storages.Transactions,
		b.storages.Receipts,
		b.publishers.Block,
		b.publishers.Transaction,
		b.publishers.Logs,
	)

	pullAPI := api.NewPullAPI(
		b.logger,
		b.config,
		b.storages.Blocks,
		b.storages.Transactions,
		b.storages.Receipts,
		ratelimiter,
	)

	debugAPI := api.NewDebugAPI(
		b.storages.Registers,
		b.storages.Traces,
		b.storages.Blocks,
		b.storages.Transactions,
		b.storages.Receipts,
		b.client,
		b.config,
		b.logger,
		b.collector,
		ratelimiter,
	)

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

	if err := b.server.EnableRPC(supportedAPIs); err != nil {
		return err
	}

	if b.config.WSEnabled {
		if err := b.server.EnableWS(supportedAPIs); err != nil {
			return err
		}
	}

	if err := b.server.SetListenAddr(b.config.RPCHost, b.config.RPCPort); err != nil {
		return err
	}

	if err := b.server.Start(); err != nil {
		return err
	}

	b.logger.Info().Msgf("API server started: %s", b.server.ListenAddr())
	return nil
}

func (b *Bootstrap) StopAPIServer() {
	if b.server == nil {
		return
	}
	b.logger.Warn().Msg("shutting down API server")
	b.server.Stop()
}

func (b *Bootstrap) StartMetricsServer(ctx context.Context) error {
	b.logger.Info().Msg("bootstrap starting metrics server")

	b.metrics = flowMetrics.NewServer(b.logger, uint(b.config.MetricsPort))
	err := util.WaitClosed(ctx, b.metrics.Ready())
	if err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}

	return nil
}

func (b *Bootstrap) StopMetricsServer() {
	if b.metrics == nil {
		return
	}
	b.logger.Warn().Msg("shutting down metrics server")
	<-b.metrics.Done()
}

func (b *Bootstrap) StartProfilerServer(_ context.Context) error {
	if !b.config.ProfilerEnabled {
		return nil
	}
	b.logger.Info().Msg("bootstrap starting profiler server")

	b.profiler = api.NewProfileServer(b.logger, b.config.ProfilerHost, b.config.ProfilerPort)

	b.profiler.Start()
	b.logger.Info().Msgf("Profiler server started: %s", b.profiler.ListenAddr())

	return nil
}

func (b *Bootstrap) StopProfilerServer() {
	if b.profiler == nil {
		return
	}

	b.logger.Warn().Msg("shutting down profiler server")

	err := b.profiler.Stop()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			b.logger.Warn().Msg("Profiler server graceful shutdown timed out")
			b.profiler.Close()
		} else {
			b.logger.Err(err).Msg("Profiler server graceful shutdown failed")
		}
	}
}

func (b *Bootstrap) StopDB() {
	if b.db == nil {
		return
	}
	err := b.db.Close()
	if err != nil {
		b.logger.Err(err).Msg("PebbleDB graceful shutdown failed")
	}
}

func (b *Bootstrap) StopClient() {
	if b.client == nil {
		return
	}
	err := b.client.Close()
	if err != nil {
		b.logger.Err(err).Msg("CrossSporkClient graceful shutdown failed")
	}
}

// StartEngine starts provided engine and panics if there are startup errors.
func StartEngine(
	ctx context.Context,
	engine models.Engine,
	logger zerolog.Logger,
) {
	l := logger.With().Type("engine", engine).Logger()

	l.Info().Msg("starting engine")
	start := time.Now()
	go func() {
		err := engine.Run(ctx)
		if err != nil {
			l.Fatal().Err(err).Msg("engine failed to run")
		}
	}()

	<-engine.Ready()
	l.Info().
		Dur("duration", time.Since(start)).
		Msg("engine started successfully")
}

// setupCrossSporkClient sets up a cross-spork AN client.
func setupCrossSporkClient(config config.Config, logger zerolog.Logger) (*requester.CrossSporkClient, error) {
	// create access client with cross-spork capabilities
	currentSporkClient, err := grpc.NewClient(
		config.AccessNodeHost,
		grpc.WithGRPCDialOptions(
			grpcOpts.WithDefaultCallOptions(grpcOpts.MaxCallRecvMsgSize(DefaultMaxMessageSize)),
			grpcOpts.WithUnaryInterceptor(retryInterceptor(
				DefaultResourceExhaustedMaxRetryDelay,
				DefaultResourceExhaustedRetryDelay,
			)),
		),
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

// retryInterceptor is a gRPC client interceptor that retries the request when the server returns
// a ResourceExhausted error
func retryInterceptor(maxDuration, pauseDuration time.Duration) grpcOpts.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpcOpts.ClientConn,
		invoker grpcOpts.UnaryInvoker,
		opts ...grpcOpts.CallOption,
	) error {
		start := time.Now()
		attempts := 0
		for {
			err := invoker(ctx, method, req, reply, cc, opts...)
			if err == nil {
				return nil
			}

			if status.Code(err) != codes.ResourceExhausted {
				return err
			}

			attempts++
			duration := time.Since(start)
			if duration >= maxDuration {
				return fmt.Errorf("request failed (attempts: %d, duration: %v): %w", attempts, duration, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pauseDuration):
			}
		}
	}
}

// setupStorage creates storage and initializes it with configured starting cadence height
// in case such a height doesn't already exist in the database.
func setupStorage(
	config config.Config,
	client *requester.CrossSporkClient,
	logger zerolog.Logger,
) (*pebbleDB.DB, *Storages, error) {
	// create pebble storage from the provided database root directory
	db, err := pebble.OpenDB(config.DatabaseDir)
	if err != nil {
		return nil, nil, err
	}
	store := pebble.New(db, logger)

	blocks := pebble.NewBlocks(store, config.FlowNetworkID)
	storageAddress := evm.StorageAccountAddress(config.FlowNetworkID)
	registerStore := pebble.NewRegisterStorage(store, storageAddress)

	batch := store.NewBatch()
	defer func() {
		err := batch.Close()
		if err != nil {
			// we don't know what went wrong, so this is fatal
			logger.Fatal().Err(err).Msg("failed to close batch")
		}
	}()

	// hard set the start cadence height, this is used when force reindexing
	if config.ForceStartCadenceHeight != 0 {
		logger.Warn().Uint64("height", config.ForceStartCadenceHeight).Msg("force setting starting Cadence height!!!")
		if err := blocks.SetLatestCadenceHeight(config.ForceStartCadenceHeight, batch); err != nil {
			return nil, nil, err
		}
	}

	// if database is not initialized require init height
	if _, err := blocks.LatestCadenceHeight(); errors.Is(err, errs.ErrStorageNotInitialized) {
		cadenceHeight := config.InitCadenceHeight
		evmBlokcHeight := uint64(0)
		cadenceBlock, err := client.GetBlockHeaderByHeight(context.Background(), cadenceHeight)
		if err != nil {
			return nil, nil, fmt.Errorf("could not fetch provided cadence height, make sure it's correct: %w", err)
		}

		snapshot, err := registerStore.GetSnapshotAt(evmBlokcHeight)
		if err != nil {
			return nil, nil, fmt.Errorf("could not get register snapshot at block height %d: %w", 0, err)
		}

		delta := storage.NewRegisterDelta(snapshot)
		accountStatus := environment.NewAccountStatus()
		err = delta.SetValue(
			storageAddress[:],
			[]byte(flowGo.AccountStatusKey),
			accountStatus.ToBytes(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("could not set account status: %w", err)
		}

		err = registerStore.Store(delta.GetUpdates(), evmBlokcHeight, batch)
		if err != nil {
			return nil, nil, fmt.Errorf("could not store register updates: %w", err)
		}

		if err := blocks.InitHeights(cadenceHeight, cadenceBlock.ID, batch); err != nil {
			return nil, nil, fmt.Errorf(
				"failed to init the database for block height: %d and ID: %s, with : %w",
				cadenceHeight,
				cadenceBlock.ID,
				err,
			)
		}

		logger.Info().
			Stringer("fvm_address_for_evm_storage_account", storageAddress).
			Msgf("database initialized with cadence height: %d", cadenceHeight)
	}
	// else {
	//	// TODO(JanezP): verify storage account owner is correct
	// }

	if batch.Count() > 0 {
		err = batch.Commit(pebbleDB.Sync)
		if err != nil {
			return nil, nil, fmt.Errorf("could not commit setup updates: %w", err)
		}
	}

	return db, &Storages{
		Storage:      store,
		Blocks:       blocks,
		Registers:    registerStore,
		Transactions: pebble.NewTransactions(store),
		Receipts:     pebble.NewReceipts(store),
		Traces:       pebble.NewTraces(store),
	}, nil
}

// Run will run complete bootstrap of the EVM gateway with all the engines.
// Run is a blocking call, but it does signal readiness of the service
// through a channel provided as an argument.
func Run(ctx context.Context, cfg config.Config, ready component.ReadyFunc) error {
	boot, err := New(cfg)
	if err != nil {
		return err
	}

	// Start the API Server first, to avoid any races with incoming
	// EVM events, that might affect the starting state.
	if err := boot.StartAPIServer(ctx); err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}

	if err := boot.StartEventIngestion(ctx); err != nil {
		return fmt.Errorf("failed to start event ingestion engine: %w", err)
	}

	if err := boot.StartMetricsServer(ctx); err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}

	if err := boot.StartProfilerServer(ctx); err != nil {
		return fmt.Errorf("failed to start profiler server: %w", err)
	}

	// mark ready
	ready()

	// if context is canceled start shutdown
	<-ctx.Done()
	boot.logger.Warn().Msg("bootstrap received context cancellation, stopping services")

	boot.StopEventIngestion()
	boot.StopMetricsServer()
	boot.StopAPIServer()
	boot.StopClient()
	boot.StopDB()

	return nil
}
