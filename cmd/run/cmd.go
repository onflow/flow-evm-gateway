package run

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	gethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
)

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Runs the EVM Gateway Node",
	RunE: func(command *cobra.Command, _ []string) error {

		ctx, cancel := context.WithCancel(command.Context())
		defer cancel()

		// create multi-key account
		// TODO(JanezP): move to separate command
		if _, exists := os.LookupEnv("MULTIKEY_MODE"); exists {
			bootstrap.RunCreateMultiKeyAccount()
			return nil
		}

		if err := parseConfigFromFlags(); err != nil {
			return fmt.Errorf("failed to parse flags: %w", err)
		}

		done := make(chan struct{})
		ready := make(chan struct{})
		once := sync.Once{}
		closeReady := func() {
			once.Do(func() {
				close(ready)
			})
		}
		go func() {
			defer close(done)
			// In case an error happens before ready is called we need to close the ready channel
			defer closeReady()

			err := bootstrap.Run(
				ctx,
				cfg,
				closeReady,
			)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Err(err).Msg("Gateway runtime error")
			}
		}()

		<-ready

		osSig := make(chan os.Signal, 1)
		signal.Notify(osSig, syscall.SIGINT, syscall.SIGTERM)

		// wait for gateway to exit or for a shutdown signal
		select {
		case <-osSig:
			log.Info().Msg("OS Signal to shutdown received, shutting down")
			cancel()
		case <-done:
			log.Info().Msg("done, shutting down")
		}

		// Wait for the gateway to completely stop
		<-done

		return nil
	},
}

func parseConfigFromFlags() error {
	if !cfg.IndexOnly {
		if coinbase == "" {
			return fmt.Errorf("coinbase EVM address required")
		}
		cfg.Coinbase = gethCommon.HexToAddress(coinbase)
		if cfg.Coinbase == (gethCommon.Address{}) {
			return fmt.Errorf("invalid coinbase address: %s", coinbase)
		}

		cfg.COAAddress = flow.HexToAddress(coa)
		if cfg.COAAddress == flow.EmptyAddress {
			return fmt.Errorf("COA address value is the empty address")
		}

		if key != "" {
			sigAlgo := crypto.StringToSignatureAlgorithm(keyAlg)
			if sigAlgo == crypto.UnknownSignatureAlgorithm {
				return fmt.Errorf("invalid signature algorithm: %s", keyAlg)
			}
			pkey, err := crypto.DecodePrivateKeyHex(sigAlgo, key)
			if err != nil {
				return fmt.Errorf("invalid COA private key: %w", err)
			}
			cfg.COAKey = pkey
		} else if cloudKMSKey != "" {
			if cloudKMSProjectID == "" || cloudKMSLocationID == "" || cloudKMSKeyRingID == "" {
				return fmt.Errorf(
					"using coa-cloud-kms-key requires also coa-cloud-kms-project-id & coa-cloud-kms-location-id & coa-cloud-kms-key-ring-id",
				)
			}

			// key has the form "{keyID}@{keyVersion}"
			keyParts := strings.Split(cloudKMSKey, "@")
			if len(keyParts) != 2 {
				return fmt.Errorf("wrong format for Cloud KMS key: %s", key)
			}
			cfg.COACloudKMSKey = &flowGoKMS.Key{
				ProjectID:  cloudKMSProjectID,
				LocationID: cloudKMSLocationID,
				KeyRingID:  cloudKMSKeyRingID,
				KeyID:      keyParts[0],
				KeyVersion: keyParts[1],
			}
		} else {
			return fmt.Errorf(
				"must either provide coa-key / coa-cloud-kms-key",
			)
		}

		if walletKey != "" {
			k, err := gethCrypto.HexToECDSA(walletKey)
			if err != nil {
				return fmt.Errorf("invalid private key for wallet API: %w", err)
			}

			cfg.WalletKey = k
			cfg.WalletEnabled = true
			log.Warn().Msg("wallet API is enabled. Ensure this is not used in production environments.")
		}
	}

	if g, ok := new(big.Int).SetString(gas, 10); ok {
		cfg.GasPrice = g
	} else {
		return fmt.Errorf("invalid gas price")
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
		cfg.InitCadenceHeight = config.TestnetInitCadenceHeight
	case "flow-mainnet":
		cfg.FlowNetworkID = flowGo.Mainnet
		cfg.EVMNetworkID = types.FlowEVMMainNetChainID
		cfg.InitCadenceHeight = config.MainnetInitCadenceHeight
	default:
		return fmt.Errorf(
			"flow network ID: %s not supported, valid values are ('flow-emulator', 'flow-previewnet', 'flow-testnet', 'flow-mainnet')",
			flowNetwork,
		)
	}

	// configure logging
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %s", logLevel)
	}
	cfg.LogLevel = level

	if logWriter == "stderr" {
		cfg.LogWriter = os.Stderr
	} else {
		cfg.LogWriter = zerolog.NewConsoleWriter()
	}

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

	switch txStateValidation {
	case config.LocalIndexValidation:
		cfg.TxStateValidation = config.LocalIndexValidation
	case config.TxSealValidation:
		cfg.TxStateValidation = config.TxSealValidation
	default:
		return fmt.Errorf("unknown tx state validation: %s", txStateValidation)
	}

	if cfg.TxBatchMode && cfg.TxBatchInterval <= 0 {
		return fmt.Errorf("tx-batch-interval must be > 0 when tx-batch-mode is enabled")
	}

	if cfg.TxBatchMode && cfg.TxStateValidation == config.TxSealValidation {
		return fmt.Errorf("tx-batch-mode should be enabled with tx-state-validation=local-index")
	}

	cfg.ExperimentalSoftFinalityEnabled = experimentalSoftFinalityEnabled
	cfg.ExperimentalSealingVerificationEnabled = experimentalSealingVerificationEnabled

	return nil
}

var cfg = config.Config{}
var (
	coinbase,
	gas,
	coa,
	key,
	keyAlg,
	flowNetwork,
	logLevel,
	logWriter,
	filterExpiry,
	accessSporkHosts,
	cloudKMSKey,
	cloudKMSProjectID,
	cloudKMSLocationID,
	cloudKMSKeyRingID,
	walletKey,
	txStateValidation string
	initHeight,
	forceStartHeight uint64

	experimentalSoftFinalityEnabled,
	experimentalSealingVerificationEnabled bool
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
	Cmd.Flags().BoolVar(&cfg.EnforceGasPrice, "enforce-gas-price", true, "Enable enforcing minimum gas price for EVM transactions. When true (default), transactions must specify a gas price greater than or equal to the configured gas price.")
	Cmd.Flags().StringVar(&coa, "coa-address", "", "Flow address that holds COA account used for submitting transactions")
	Cmd.Flags().StringVar(&key, "coa-key", "", "Private key value for the COA address used for submitting transactions")
	Cmd.Flags().StringVar(&keyAlg, "coa-key-alg", "ECDSA_P256", "Private key algorithm for the COA private key, only effective if coa-key/coa-key-file is present. Available values (ECDSA_P256 / ECDSA_secp256k1 / BLS_BLS12_381), defaults to ECDSA_P256.")
	Cmd.Flags().StringVar(&logLevel, "log-level", "debug", "Define verbosity of the log output ('debug', 'info', 'warn', 'error', 'fatal', 'panic')")
	Cmd.Flags().StringVar(&logWriter, "log-writer", "stderr", "Log writer used for output ('stderr', 'console')")
	Cmd.Flags().Uint64Var(&cfg.RateLimit, "rate-limit", 50, "Rate-limit requests per second made by the client over any protocol (ws/http)")
	Cmd.Flags().StringVar(&cfg.AddressHeader, "address-header", "", "Address header that contains the client IP, this is useful when the server is behind a proxy that sets the source IP of the client. Leave empty if no proxy is used.")
	Cmd.Flags().Uint64Var(&forceStartHeight, "force-start-height", 0, "Force set starting Cadence height. WARNING: This should only be used locally or for testing, never in production.")
	Cmd.Flags().StringVar(&filterExpiry, "filter-expiry", "5m", "Filter defines the time it takes for an idle filter to expire")
	Cmd.Flags().StringVar(&cloudKMSProjectID, "coa-cloud-kms-project-id", "", "The project ID containing the KMS keys, e.g. 'flow-evm-gateway'")
	Cmd.Flags().StringVar(&cloudKMSLocationID, "coa-cloud-kms-location-id", "", "The location ID where the key ring is grouped into, e.g. 'global'")
	Cmd.Flags().StringVar(&cloudKMSKeyRingID, "coa-cloud-kms-key-ring-id", "", "The key ring ID where the KMS keys exist, e.g. 'tx-signing'")
	Cmd.Flags().StringVar(&cloudKMSKey, "coa-cloud-kms-key", "", `Name of the KMS key and its version, e.g. "gw-key-6@1"`)
	Cmd.Flags().BoolVar(&cfg.COATxLookupEnabled, "coa-tx-lookup-enabled", false, "Tracks cadence transactions to release COA signing keys more quickly. Use this on nodes with high tx volume that frequently run out of proposer keys.")
	Cmd.Flags().StringVar(&walletKey, "wallet-api-key", "", "ECDSA private key used for wallet APIs. WARNING: This should only be used locally or for testing, never in production.")
	Cmd.Flags().IntVar(&cfg.MetricsPort, "metrics-port", 9091, "Port for the metrics server")
	Cmd.Flags().BoolVar(&cfg.IndexOnly, "index-only", false, "Run the gateway in index-only mode which only allows querying the state and indexing, but disallows sending transactions.")
	Cmd.Flags().BoolVar(&cfg.ProfilerEnabled, "profiler-enabled", false, "Run the profiler server to capture pprof data.")
	Cmd.Flags().StringVar(&cfg.ProfilerHost, "profiler-host", "localhost", "Host for the Profiler server")
	Cmd.Flags().IntVar(&cfg.ProfilerPort, "profiler-port", 6060, "Port for the Profiler server")
	Cmd.Flags().StringVar(&txStateValidation, "tx-state-validation", "tx-seal", "Sets the transaction validation mechanism. It can validate using the local state index, or wait for the outer Flow transaction to seal. Available values ('local-index' / 'tx-seal'), defaults to 'tx-seal'.")
	Cmd.Flags().Uint64Var(&cfg.TxRequestLimit, "tx-request-limit", 0, "Number of transaction submissions to allow per the specified interval.")
	Cmd.Flags().DurationVar(&cfg.TxRequestLimitDuration, "tx-request-limit-duration", time.Second*3, "Time interval upon which to enforce transaction submission rate limiting.")
	Cmd.Flags().BoolVar(&cfg.TxBatchMode, "tx-batch-mode", false, "Enable batch transaction submission, to avoid nonce mismatch issues for high-volume EOAs.")
	Cmd.Flags().DurationVar(&cfg.TxBatchInterval, "tx-batch-interval", time.Millisecond*1200, "Time interval upon which to submit the transaction batches to the Flow network.")
	Cmd.Flags().BoolVar(&experimentalSoftFinalityEnabled, "experimental-soft-finality-enabled", false, "Sets whether the gateway should use the experimental soft finality feature. WARNING: This may result in incorrect results being returned in certain circumstances. Use only if you know what you are doing.")
	Cmd.Flags().BoolVar(&experimentalSealingVerificationEnabled, "experimental-sealing-verification-enabled", true, "Sets whether the gateway should use the experimental soft finality sealing verification feature. WARNING: This may result in indexing halts if events do not match. Use only if you know what you are doing.")
	Cmd.Flags().DurationVar(&cfg.EOAActivityCacheTTL, "eoa-activity-cache-ttl", time.Second*10, "Time interval used to track EOA activity. Tx send more frequently than this interval will be batched. Useful only when batch transaction submission is enabled.")
	Cmd.Flags().DurationVar(&cfg.RpcRequestTimeout, "rpc-request-timeout", time.Second*120, "Sets the maximum duration at which JSON-RPC requests should generate a response, before they timeout. The default is 120 seconds.")

	err := Cmd.Flags().MarkDeprecated("init-cadence-height", "This flag is no longer necessary and will be removed in future version. The initial Cadence height is known for testnet/mainnet and this was only required for fresh deployments of EVM Gateway. Once the DB has been initialized, the latest index Cadence height will be used upon start-up.")
	if err != nil {
		panic(err)
	}
}
