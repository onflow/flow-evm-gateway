package run

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethCrypto "github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Runs the EVM Gateway Node",
	Run: func(*cobra.Command, []string) {
		// create multi-key account
		if _, exists := os.LookupEnv("MULTIKEY_MODE"); exists {
			bootstrap.RunCreateMultiKeyAccount()
			return
		}

		if err := parseConfigFromFlags(); err != nil {
			log.Err(err).Msg("failed to parse flags")
			os.Exit(1)
		}

		ctx, cancel := context.WithCancel(context.Background())
		ready := make(chan struct{})
		go func() {
			if err := bootstrap.Run(ctx, cfg, ready); err != nil {
				log.Err(err).Msg("failed to run bootstrap")
				cancel()
				os.Exit(1)
			}
		}()

		<-ready

		osSig := make(chan os.Signal, 1)
		signal.Notify(osSig, syscall.SIGINT, syscall.SIGTERM)

		<-osSig
		log.Info().Msg("OS Signal to shutdown received, shutting down")
		cancel()
	},
}

func parseConfigFromFlags() error {
	if coinbase == "" {
		return fmt.Errorf("coinbase EVM address required")
	}
	cfg.Coinbase = gethCommon.HexToAddress(coinbase)

	if g, ok := new(big.Int).SetString(gas, 10); ok {
		cfg.GasPrice = g
	} else if !ok {
		return fmt.Errorf("invalid gas price")
	}

	cfg.COAAddress = flow.HexToAddress(coa)
	if cfg.COAAddress == flow.EmptyAddress {
		return fmt.Errorf("COA address value is the empty address")
	}

	if key != "" {
		sigAlgo := crypto.StringToSignatureAlgorithm(keyAlg)
		pkey, err := crypto.DecodePrivateKeyHex(sigAlgo, key)
		if err != nil {
			return fmt.Errorf("invalid COA private key: %w", err)
		}
		cfg.COAKey = pkey
	} else if keysPath != "" {
		raw, err := os.ReadFile(keysPath)
		if err != nil {
			return fmt.Errorf("could not read the file containing list of keys for key-rotation mechanism, check if coa-key-file specifies valid path: %w", err)
		}
		var keysJSON []string
		if err := json.Unmarshal(raw, &keysJSON); err != nil {
			return fmt.Errorf("could not parse file containing the list of keys for key-rotation, make sure keys are in JSON array format: %w", err)
		}

		cfg.COAKeys = make([]crypto.PrivateKey, len(keysJSON))
		sigAlgo := crypto.StringToSignatureAlgorithm(keyAlg)
		for i, k := range keysJSON {
			pk, err := crypto.DecodePrivateKeyHex(sigAlgo, k)
			if err != nil {
				return fmt.Errorf("a key from the COA key list file is not valid, key %s, error: %w", k, err)
			}
			cfg.COAKeys[i] = pk
		}
	} else if cloudKMSKeys != "" {
		if cloudKMSProjectID == "" || cloudKMSLocationID == "" || cloudKMSKeyRingID == "" {
			return fmt.Errorf(
				"using coa-cloud-kms-keys requires also coa-cloud-kms-project-id & coa-cloud-kms-location-id & coa-cloud-kms-key-ring-id",
			)
		}

		kmsKeys := strings.Split(cloudKMSKeys, ",")
		cfg.COACloudKMSKeys = make([]flowGoKMS.Key, len(kmsKeys))
		for i, key := range kmsKeys {
			// key has the form "{keyID}@{keyVersion}"
			keyParts := strings.Split(key, "@")
			if len(keyParts) != 2 {
				return fmt.Errorf("wrong format for Cloud KMS key: %s", key)
			}
			cfg.COACloudKMSKeys[i] = flowGoKMS.Key{
				ProjectID:  cloudKMSProjectID,
				LocationID: cloudKMSLocationID,
				KeyRingID:  cloudKMSKeyRingID,
				KeyID:      keyParts[0],
				KeyVersion: keyParts[1],
			}
		}
	} else {
		return fmt.Errorf(
			"must either provide coa-key / coa-key-path / coa-cloud-kms-keys",
		)
	}

	switch flowNetwork {
	case "flow-previewnet":
		cfg.FlowNetworkID = flowGo.Previewnet
		cfg.EVMNetworkID = types.FlowEVMPreviewNetChainID
		cfg.InitCadenceHeight = config.LiveNetworkInitCadenceHeight
	case "flow-emulator":
		cfg.FlowNetworkID = flowGo.Emulator
		cfg.EVMNetworkID = types.FlowEVMPreviewNetChainID
		cfg.InitCadenceHeight = config.EmulatorInitCadenceHeight
	case "flow-testnet":
		cfg.FlowNetworkID = flowGo.Testnet
		cfg.EVMNetworkID = types.FlowEVMTestNetChainID
		cfg.InitCadenceHeight = config.LiveNetworkInitCadenceHeight
	case "flow-mainnet":
		cfg.FlowNetworkID = flowGo.Mainnet
		cfg.EVMNetworkID = types.FlowEVMMainNetChainID
		cfg.InitCadenceHeight = config.LiveNetworkInitCadenceHeight
	default:
		return fmt.Errorf(
			"flow network ID: %s not supported, valid values are ('flow-emulator', 'flow-previewnet', 'flow-testnet', 'flow-mainnet')",
			flowNetwork,
		)
	}

	// if a specific value was provided use it
	if initHeight != 0 {
		cfg.InitCadenceHeight = initHeight
	}

	// configure logging
	switch logLevel {
	case "debug":
		cfg.LogLevel = zerolog.DebugLevel
	case "info":
		cfg.LogLevel = zerolog.InfoLevel
	case "warn":
		cfg.LogLevel = zerolog.WarnLevel
	case "error":
		cfg.LogLevel = zerolog.ErrorLevel
	case "fatal":
		cfg.LogLevel = zerolog.FatalLevel
	case "panic":
		cfg.LogLevel = zerolog.PanicLevel
	default:
		return fmt.Errorf("invalid log level: %s", logLevel)

	}

	if logWriter == "stderr" {
		cfg.LogWriter = os.Stderr
	} else {
		cfg.LogWriter = zerolog.NewConsoleWriter()
	}

	cfg.StreamTimeout = time.Second * time.Duration(streamTimeout)

	exp, err := time.ParseDuration(filterExpiry)
	if err != nil {
		return fmt.Errorf("invalid unit %s for filter expiry: %w", filterExpiry, err)
	}
	cfg.FilterExpiry = exp

	if accessSporkHosts != "" {
		heightHosts := strings.Split(accessSporkHosts, ",")
		cfg.AccessNodePreviousSporkHosts = append(cfg.AccessNodePreviousSporkHosts, heightHosts...)
	}

	if forceStartHeight != 0 {
		cfg.ForceStartCadenceHeight = forceStartHeight
	}

	cfg.TracesEnabled = cfg.TracesBucketName != ""

	if cfg.TracesBackfillStartHeight > 0 && cfg.TracesBackfillEndHeight > 0 && cfg.TracesBackfillStartHeight > cfg.TracesBackfillEndHeight {
		return fmt.Errorf("traces backfill start height must be less than the end height")
	}

	if walletKey != "" {
		k, err := gethCrypto.HexToECDSA(walletKey)
		if err != nil {
			return fmt.Errorf("invalid private key for wallet API: %w", err)
		}

		cfg.WalletKey = k
		cfg.WalletEnabled = true
	}

	return nil
}

var cfg = &config.Config{}
var (
	coinbase,
	gas,
	coa,
	key,
	keyAlg,
	keysPath,
	flowNetwork,
	logLevel,
	logWriter,
	filterExpiry,
	accessSporkHosts,
	cloudKMSKeys,
	cloudKMSProjectID,
	cloudKMSLocationID,
	cloudKMSKeyRingID,
	walletKey string

	streamTimeout int

	initHeight,
	forceStartHeight uint64
)

func init() {
	// Set all available flags
	Cmd.Flags().StringVar(&cfg.DatabaseDir, "database-dir", "./db", "Path to the directory for the database")
	Cmd.Flags().StringVar(&cfg.RPCHost, "rpc-host", "", "Host for the RPC API server")
	Cmd.Flags().IntVar(&cfg.RPCPort, "rpc-port", 8545, "Port for the RPC API server")
	Cmd.Flags().BoolVar(&cfg.WSEnabled, "ws-enabled", false, "Enable websocket connections")
	Cmd.Flags().StringVar(&cfg.AccessNodeHost, "access-node-grpc-host", "localhost:3569", "Host to the flow access node gRPC API")
	Cmd.Flags().StringVar(&accessSporkHosts, "access-node-spork-hosts", "", `Previous spork AN hosts, defined following the schema: {host1},{host2} as a comma separated list (e.g. "host-1.com,host2.com")`)
	Cmd.Flags().StringVar(&flowNetwork, "flow-network-id", "flow-emulator", "Flow network ID (flow-emulator, flow-previewnet, flow-testnet, flow-mainnet)")
	Cmd.Flags().StringVar(&coinbase, "coinbase", "", "Coinbase address to use for fee collection")
	Cmd.Flags().Uint64Var(&initHeight, "init-cadence-height", 0, "Define the Cadence block height at which to start the indexing, if starting on a new network this flag should not be used.")
	Cmd.Flags().StringVar(&gas, "gas-price", "1", "Static gas price used for EVM transactions")
	Cmd.Flags().StringVar(&coa, "coa-address", "", "Flow address that holds COA account used for submitting transactions")
	Cmd.Flags().StringVar(&key, "coa-key", "", "Private key value for the COA address used for submitting transactions")
	Cmd.Flags().StringVar(&keyAlg, "coa-key-alg", "ECDSA_P256", "Private key algorithm for the COA private key, only effective if coa-key/coa-key-file is present. Available values (ECDSA_P256 / ECDSA_secp256k1 / BLS_BLS12_381), defaults to ECDSA_P256.")
	Cmd.Flags().StringVar(&keysPath, "coa-key-file", "", "File path that contains JSON array of COA keys used in key-rotation mechanism, this is exclusive with coa-key flag.")
	Cmd.Flags().BoolVar(&cfg.CreateCOAResource, "coa-resource-create", false, "Auto-create the COA resource in the Flow COA account provided if one doesn't exist")
	Cmd.Flags().StringVar(&logLevel, "log-level", "debug", "Define verbosity of the log output ('debug', 'info', 'warn', 'error', 'fatal', 'panic')")
	Cmd.Flags().StringVar(&logWriter, "log-writer", "stderr", "Log writer used for output ('stderr', 'console')")
	Cmd.Flags().Float64Var(&cfg.StreamLimit, "stream-limit", 10, "Rate-limits the events sent to the client within one second")
	Cmd.Flags().Uint64Var(&cfg.RateLimit, "rate-limit", 50, "Rate-limit requests per second made by the client over any protocol (ws/http)")
	Cmd.Flags().StringVar(&cfg.AddressHeader, "address-header", "", "Address header that contains the client IP, this is useful when the server is behind a proxy that sets the source IP of the client. Leave empty if no proxy is used.")
	Cmd.Flags().Uint64Var(&cfg.HeartbeatInterval, "heartbeat-interval", 100, "Heartbeat interval for AN event subscription")
	Cmd.Flags().UintVar(&cfg.CacheSize, "script-cache-size", 10000, "Cache size used for script execution in items kept in cache")
	Cmd.Flags().IntVar(&streamTimeout, "stream-timeout", 3, "Defines the timeout in seconds the server waits for the event to be sent to the client")
	Cmd.Flags().Uint64Var(&forceStartHeight, "force-start-height", 0, "Force set starting Cadence height. WARNING: This should only be used locally or for testing, never in production.")
	Cmd.Flags().StringVar(&filterExpiry, "filter-expiry", "5m", "Filter defines the time it takes for an idle filter to expire")
	Cmd.Flags().StringVar(&cfg.TracesBucketName, "traces-gcp-bucket", "", "GCP bucket name where transaction traces are stored")
	Cmd.Flags().Uint64Var(&cfg.TracesBackfillStartHeight, "traces-backfill-start-height", 0, "evm block height from which to start backfilling missing traces.")
	Cmd.Flags().Uint64Var(&cfg.TracesBackfillEndHeight, "traces-backfill-end-height", 0, "evm block height until which to backfill missing traces. If 0, backfill until the latest block")
	Cmd.Flags().StringVar(&cloudKMSProjectID, "coa-cloud-kms-project-id", "", "The project ID containing the KMS keys, e.g. 'flow-evm-gateway'")
	Cmd.Flags().StringVar(&cloudKMSLocationID, "coa-cloud-kms-location-id", "", "The location ID where the key ring is grouped into, e.g. 'global'")
	Cmd.Flags().StringVar(&cloudKMSKeyRingID, "coa-cloud-kms-key-ring-id", "", "The key ring ID where the KMS keys exist, e.g. 'tx-signing'")
	Cmd.Flags().StringVar(&cloudKMSKeys, "coa-cloud-kms-keys", "", `Names of the KMS keys and their versions as a comma separated list, e.g. "gw-key-6@1,gw-key-7@1,gw-key-8@1"`)
	Cmd.Flags().StringVar(&walletKey, "wallet-api-key", "", "ECDSA private key used for wallet APIs. WARNING: This should only be used locally or for testing, never in production.")
	Cmd.Flags().IntVar(&cfg.MetricsPort, "metrics-port", 9091, "Port for the metrics server")
	Cmd.Flags().BoolVar(&cfg.IndexOnly, "index-only", false, "Run the gateway in index-only mode which only allows querying the state and indexing, but disallows sending transactions.")
	Cmd.Flags().BoolVar(&cfg.ProfilerEnabled, "profiler-enabled", false, "Run the profiler server to capture pprof data.")
	Cmd.Flags().StringVar(&cfg.ProfilerHost, "profiler-host", "localhost", "Host for the Profiler server")
	Cmd.Flags().IntVar(&cfg.ProfilerPort, "profiler-port", 6060, "Port for the Profiler server")
}
