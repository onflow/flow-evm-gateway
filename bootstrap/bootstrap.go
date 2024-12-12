package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/access/grpc"
	grpcOpts "google.golang.org/grpc"

	"github.com/cockroachdb/pebble"
	"github.com/hashicorp/go-multierror"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/cmd"
	"github.com/onflow/flow-go/fvm/environment"
	"github.com/onflow/flow-go/fvm/evm"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/module"
	"github.com/onflow/flow-go/module/component"
	"github.com/onflow/flow-go/module/metrics"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter/memorystore"

	metrics2 "github.com/onflow/flow-evm-gateway/metrics"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/services/signer"
	pebble2 "github.com/onflow/flow-evm-gateway/storage/pebble"
)

type Storages struct {
	Storage      *pebble2.Storage
	Registers    *pebble2.RegisterStorage
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

type EVMGatewayNodeImp struct {
	cmd.NodeImp
	config.Config
}

// NewNode returns a new node instance
func NewNode(
	component component.Component,
	cfg config.Config,
	logger zerolog.Logger,
	cleanup func() error,
	handleFatal func(error),
) *EVMGatewayNodeImp {
	return &EVMGatewayNodeImp{
		Config: cfg,
		NodeImp: cmd.NewBaseNode(
			component,
			logger.With().
				Str("node_role", "EVM Gateway").
				Logger(),
			cleanup,
			handleFatal,
		),
	}
}

type EVMGatewayNodeBuilder struct {
	config.Config
	Logger           zerolog.Logger
	componentBuilder component.ComponentManagerBuilder
	components       []cmd.NamedComponentFactory[config.Config]
	postShutdownFns  []func() error
	modules          []namedModuleFunc

	Metrics  metrics2.Collector
	DB       *pebble.DB
	Client   *requester.CrossSporkClient
	Storages *Storages
	// Signer is used for signing flow transactions
	Signer     crypto.Signer
	Publishers *Publishers
}

func (fnb *EVMGatewayNodeBuilder) Build() (cmd.Node, error) {
	// Run the prestart initialization. This includes anything that should be done before
	// starting the components.
	if err := fnb.onStart(); err != nil {
		return nil, err
	}

	return NewNode(
		fnb.componentBuilder.Build(),
		fnb.Config,
		fnb.Logger,
		fnb.postShutdown,
		fnb.handleFatal,
	), nil
}

func (fnb *EVMGatewayNodeBuilder) onStart() error {

	if err := fnb.initDB(); err != nil {
		return err
	}

	if err := fnb.initMetrics(); err != nil {
		return err
	}

	if err := fnb.initClient(); err != nil {
		return err
	}

	if err := fnb.initStorage(); err != nil {
		return err
	}

	// run all modules
	if err := fnb.handleModules(); err != nil {
		return fmt.Errorf("could not handle modules: %w", err)
	}

	// run all components
	return fnb.handleComponents()
}

func (fnb *EVMGatewayNodeBuilder) initDB() error {
	pebbleDB, err := pebble2.OpenDB(fnb.DatabaseDir)
	if err != nil {
		return fmt.Errorf("failed to open db for dir: %s, with: %w", fnb.DatabaseDir, err)
	}

	fnb.DB = pebbleDB

	fnb.ShutdownFunc(func() error {
		if err := fnb.DB.Close(); err != nil {
			return fmt.Errorf("error closing pebble database: %w", err)
		}
		return nil
	})

	return err
}

func (fnb *EVMGatewayNodeBuilder) Component(name string, f cmd.ReadyDoneFactory[config.Config]) *EVMGatewayNodeBuilder {
	fnb.components = append(fnb.components, cmd.NamedComponentFactory[config.Config]{
		ComponentFactory: f,
		Name:             name,
	})
	return fnb
}

// postShutdown is called by the node before exiting
// put any cleanup code here that should be run after all components have stopped
func (fnb *EVMGatewayNodeBuilder) postShutdown() error {
	var errs *multierror.Error

	for _, fn := range fnb.postShutdownFns {
		err := fn()
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	fnb.Logger.Info().Msg("database has been closed")
	return errs.ErrorOrNil()
}

// handleFatal handles irrecoverable errors by logging them and exiting the process.
func (fnb *EVMGatewayNodeBuilder) handleFatal(err error) {
	fnb.Logger.Fatal().Err(err).Msg("unhandled irrecoverable error")
}

func NewEVMGatewayNodeBuilder(
	config config.Config,
) *EVMGatewayNodeBuilder {

	logger := zerolog.New(config.LogWriter).
		With().Timestamp().Str("version", api.Version).
		Logger().Level(config.LogLevel)

	return &EVMGatewayNodeBuilder{
		Logger:           logger,
		Config:           config,
		componentBuilder: component.NewComponentManagerBuilder(),
	}
}

func (fnb *EVMGatewayNodeBuilder) Initialize() error {
	fnb.PrintBuildDetails()

	fnb.EnqueueMetricsServerInit()

	return nil
}

func (fnb *EVMGatewayNodeBuilder) LoadComponentsAndModules() {
	fnb.initPublishers()

	fnb.Component("Transaction Signer", fnb.initSigner)
	fnb.Component("API Server", fnb.apiServerComponent)
	fnb.Component("Event Ingestion Engine", fnb.eventIngestionEngineComponent)
	fnb.Component("Metrics Server", fnb.metricsServerComponent)
	fnb.Component("Profiler Server", fnb.profilerServerComponent)
}

func (fnb *EVMGatewayNodeBuilder) metricsServerComponent(config config.Config) (module.ReadyDoneAware, error) {
	server := metrics.NewServer(fnb.Logger, uint(config.MetricsPort))
	return server, nil
}

func (fnb *EVMGatewayNodeBuilder) profilerServerComponent(config config.Config) (module.ReadyDoneAware, error) {
	server := api.NewProfileServer(fnb.Logger, config.ProfilerHost, config.ProfilerPort)
	return server, nil
}

func (fnb *EVMGatewayNodeBuilder) apiServerComponent(cfg config.Config) (module.ReadyDoneAware, error) {
	log := fnb.Logger

	log.Info().Msg("bootstrap starting Metrics server")

	server := api.NewServer(log, fnb.Metrics, cfg)

	// create transaction pool
	txPool := requester.NewTxPool(
		fnb.Client,
		fnb.Publishers.Transaction,
		log,
	)

	blocksProvider := replayer.NewBlocksProvider(
		fnb.Storages.Blocks,
		cfg.FlowNetworkID,
		nil,
	)

	evm, err := requester.NewEVM(
		fnb.Storages.Registers,
		blocksProvider,
		fnb.Client,
		cfg,
		fnb.Signer,
		log,
		fnb.Storages.Blocks,
		txPool,
		fnb.Metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create EVM requester: %w", err)
	}

	// create rate limiter for requests on the APIs. Tokens are number of requests allowed per 1 second interval
	// if no limit is defined we specify max value, effectively disabling rate-limiting
	rateLimit := cfg.RateLimit
	if rateLimit == 0 {
		log.Warn().Msg("no rate-limiting is set")
		rateLimit = math.MaxInt
	}
	ratelimiter, err := memorystore.New(&memorystore.Config{Tokens: rateLimit, Interval: time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to create rate limiter: %w", err)
	}

	// get the height from which the indexing resumed since the last restart,
	// this is needed for the `eth_syncing` endpoint.
	indexingResumedHeight, err := fnb.Storages.Blocks.LatestEVMHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve the indexing resumed height: %w", err)
	}

	blockchainAPI := api.NewBlockChainAPI(
		log,
		cfg,
		evm,
		fnb.Storages.Blocks,
		fnb.Storages.Transactions,
		fnb.Storages.Receipts,
		ratelimiter,
		fnb.Metrics,
		indexingResumedHeight,
	)

	streamAPI := api.NewStreamAPI(
		log,
		cfg,
		fnb.Storages.Blocks,
		fnb.Storages.Transactions,
		fnb.Storages.Receipts,
		fnb.Publishers.Block,
		fnb.Publishers.Transaction,
		fnb.Publishers.Logs,
	)

	pullAPI := api.NewPullAPI(
		log,
		cfg,
		fnb.Storages.Blocks,
		fnb.Storages.Transactions,
		fnb.Storages.Receipts,
		ratelimiter,
	)

	debugAPI := api.NewDebugAPI(
		fnb.Storages.Registers,
		fnb.Storages.Traces,
		fnb.Storages.Blocks,
		fnb.Storages.Transactions,
		fnb.Storages.Receipts,
		fnb.Client,
		cfg,
		log,
		fnb.Metrics,
	)

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

	if err := server.EnableRPC(supportedAPIs); err != nil {
		return nil, err
	}

	if cfg.WSEnabled {
		if err := server.EnableWS(supportedAPIs); err != nil {
			return nil, err
		}
	}

	if err := server.SetListenAddr(cfg.RPCHost, cfg.RPCPort); err != nil {
		return nil, err
	}

	return server, nil
}

func (fnb *EVMGatewayNodeBuilder) eventIngestionEngineComponent(cfg config.Config) (module.ReadyDoneAware, error) {
	l := fnb.Logger.With().Str("component", "bootstrap-ingestion").Logger()
	l.Info().Msg("bootstrap starting event ingestion")

	// get latest cadence block from the network and the database
	latestCadenceBlock, err := fnb.Client.GetLatestBlock(context.Background(), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest cadence block: %w", err)
	}

	latestCadenceHeight, err := fnb.Storages.Blocks.LatestCadenceHeight()
	if err != nil {
		return nil, err
	}

	// make sure the provided block to start the indexing can be loaded
	_, err = fnb.Client.GetBlockHeaderByHeight(context.Background(), latestCadenceHeight)
	if err != nil {
		return nil, fmt.Errorf(
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

	chainID := cfg.FlowNetworkID

	// create event subscriber
	subscriber := ingestion.NewRPCEventSubscriber(
		fnb.Logger,
		fnb.Client,
		chainID,
		latestCadenceHeight,
	)

	callTracerCollector, err := replayer.NewCallTracerCollector(fnb.Logger)
	if err != nil {
		return nil, err
	}
	blocksProvider := replayer.NewBlocksProvider(
		fnb.Storages.Blocks,
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
	events := ingestion.NewEventIngestionEngine(
		subscriber,
		blocksProvider,
		fnb.Storages.Storage,
		fnb.Storages.Registers,
		fnb.Storages.Blocks,
		fnb.Storages.Receipts,
		fnb.Storages.Transactions,
		fnb.Storages.Traces,
		fnb.Publishers.Block,
		fnb.Publishers.Logs,
		fnb.Logger,
		fnb.Metrics,
		replayerConfig,
	)

	return events, nil
}

func (fnb *EVMGatewayNodeBuilder) PrintBuildDetails() {
	fnb.Logger.Info().Str("version", api.Version).Msg("build details")
}

// ShutdownFunc adds a callback function that is called after all components have exited.
func (fnb *EVMGatewayNodeBuilder) ShutdownFunc(fn func() error) *EVMGatewayNodeBuilder {
	fnb.postShutdownFns = append(fnb.postShutdownFns, fn)
	return fnb
}

func (fnb *EVMGatewayNodeBuilder) EnqueueMetricsServerInit() {
	fnb.Component("Metrics server", func(config config.Config) (module.ReadyDoneAware, error) {
		server := metrics.NewServer(fnb.Logger, uint(config.MetricsPort))
		return server, nil
	})
}

func (fnb *EVMGatewayNodeBuilder) initMetrics() error {
	fnb.Metrics = metrics2.NewCollector(fnb.Logger)
	return nil
}

func (fnb *EVMGatewayNodeBuilder) initStorage() error {
	logger := fnb.Logger
	cfg := fnb.Config

	store := pebble2.New(fnb.DB, fnb.Logger)

	blocks := pebble2.NewBlocks(store, cfg.FlowNetworkID)
	storageAddress := evm.StorageAccountAddress(cfg.FlowNetworkID)
	registerStore := pebble2.NewRegisterStorage(store, storageAddress)

	// hard set the start cadence height, this is used when force reindexing
	if cfg.ForceStartCadenceHeight != 0 {
		logger.Warn().Uint64("height", cfg.ForceStartCadenceHeight).Msg("force setting starting Cadence height!!!")
		if err := blocks.SetLatestCadenceHeight(cfg.ForceStartCadenceHeight, nil); err != nil {
			return err
		}
	}

	// if database is not initialized require init height
	if _, err := blocks.LatestCadenceHeight(); errors.Is(err, errs.ErrStorageNotInitialized) {
		// TODO(JanezP): move this to a separate function
		err = func() (innerErr error) {
			batch := store.NewBatch()
			defer func(batch *pebble.Batch) {
				innerErr = batch.Close()
			}(batch)

			cadenceHeight := cfg.InitCadenceHeight
			evmBlokcHeight := uint64(0)
			cadenceBlock, err := fnb.Client.GetBlockHeaderByHeight(context.Background(), cadenceHeight)
			if err != nil {
				return fmt.Errorf("could not fetch provided cadence height, make sure it's correct: %w", err)
			}

			snapshot, err := registerStore.GetSnapshotAt(evmBlokcHeight)
			if err != nil {
				return fmt.Errorf("could not get register snapshot at block height %d: %w", 0, err)
			}

			delta := storage.NewRegisterDelta(snapshot)
			accountStatus := environment.NewAccountStatus()
			err = delta.SetValue(
				storageAddress[:],
				[]byte(flowGo.AccountStatusKey),
				accountStatus.ToBytes(),
			)
			if err != nil {
				return fmt.Errorf("could not set account status: %w", err)
			}

			err = registerStore.Store(delta.GetUpdates(), evmBlokcHeight, batch)
			if err != nil {
				return fmt.Errorf("could not store register updates: %w", err)
			}

			if err := blocks.InitHeights(cadenceHeight, cadenceBlock.ID, batch); err != nil {
				return fmt.Errorf(
					"failed to init the database for block height: %d and ID: %s, with : %w",
					cadenceHeight,
					cadenceBlock.ID,
					err,
				)
			}

			err = batch.Commit(pebble.Sync)
			if err != nil {
				return fmt.Errorf("could not commit register updates: %w", err)
			}

			logger.Info().
				Stringer("fvm_address_for_evm_storage_account", storageAddress).
				Msgf("database initialized with cadence height: %d", cadenceHeight)

			return nil
		}()

		if err != nil {
			return fmt.Errorf("failed to init the database: %w", err)
		}
	}
	//else {
	//	// TODO(JanezP): verify storage account owner is correct
	//}

	fnb.Storages = &Storages{
		Storage:      store,
		Blocks:       blocks,
		Registers:    registerStore,
		Transactions: pebble2.NewTransactions(store),
		Receipts:     pebble2.NewReceipts(store),
		Traces:       pebble2.NewTraces(store),
	}

	return nil
}
func (fnb *EVMGatewayNodeBuilder) initSigner(config config.Config) (module.ReadyDoneAware, error) {
	sig := signer.NewSigner(fnb.Logger, config)
	fnb.Signer = sig
	return sig, nil
}

func (fnb *EVMGatewayNodeBuilder) initPublishers() {
	fnb.Publishers = &Publishers{}

	fnb.Component("Block Publisher", func(config config.Config) (module.ReadyDoneAware, error) {
		p := models.NewPublisher[*models.Block](fnb.Logger)
		fnb.Publishers.Block = p
		return p, nil
	})
	fnb.Component("Transaction Publisher", func(config config.Config) (module.ReadyDoneAware, error) {
		p := models.NewPublisher[*gethTypes.Transaction](fnb.Logger)
		fnb.Publishers.Transaction = p
		return p, nil
	})
	fnb.Component("Logs Publisher", func(config config.Config) (module.ReadyDoneAware, error) {
		p := models.NewPublisher[[]*gethTypes.Log](fnb.Logger)
		fnb.Publishers.Logs = p
		return p, nil
	})
}

func (fnb *EVMGatewayNodeBuilder) initClient() error {
	logger := fnb.Logger
	cfg := fnb.Config

	client, err := setupCrossSporkClient(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create cross-spork client: %w", err)
	}

	fnb.Client = client

	fnb.ShutdownFunc(func() error {
		if err := fnb.Client.Close(); err != nil {
			return fmt.Errorf("error closing cross-spork client: %w", err)
		}
		return nil
	})

	return nil
}

type BuilderFunc func(nodeConfig config.Config) error

type namedModuleFunc struct {
	fn   BuilderFunc
	name string
}

// handleModules initializes the given module.
func (fnb *EVMGatewayNodeBuilder) handleModule(v namedModuleFunc) error {
	fnb.Logger.Info().Str("module", v.name).Msg("module initialization started")
	err := v.fn(fnb.Config)
	if err != nil {
		return fmt.Errorf("module %s initialization failed: %w", v.name, err)
	}

	fnb.Logger.Info().Str("module", v.name).Msg("module initialization complete")
	return nil
}

// handleModules initializes all modules that have been enqueued on this node builder.
func (fnb *EVMGatewayNodeBuilder) handleModules() error {
	for _, f := range fnb.modules {
		if err := fnb.handleModule(f); err != nil {
			return err
		}
	}

	return nil
}

func (fnb *EVMGatewayNodeBuilder) handleComponents() error {
	cmd.AddWorkersFromComponents(fnb.Logger, fnb.Config, fnb.componentBuilder, fnb.components)
	return nil
}

// setupCrossSporkClient sets up a cross-spork AN client.
func setupCrossSporkClient(config config.Config, logger zerolog.Logger) (*requester.CrossSporkClient, error) {
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
