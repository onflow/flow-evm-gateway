package config

import (
	"crypto/ecdsa"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	gethCrypto "github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
)

// Default InitCadenceHeight for initializing the database on a local emulator.
// TODO: temporary fix until https://github.com/onflow/flow-go/issues/5481 is
// fixed upstream and released.
const EmulatorInitCadenceHeight = uint64(0)

// Default InitCadenceHeight for initializing the database on a live network.
// We don't use 0 as it has a special meaning to represent latest block in the AN API context.
const LiveNetworkInitCadenceHeght = uint64(1)

type Config struct {
	// DatabaseDir is where the database should be stored.
	DatabaseDir string
	// AccessNodeHost defines the current spork Flow network AN host.
	AccessNodeHost string
	// AccessNodePreviousSporkHosts contains a list of the ANs hosts for each spork
	AccessNodePreviousSporkHosts []string
	// GRPCPort for the RPC API server
	RPCPort int
	// GRPCHost for the RPC API server
	RPCHost string
	// WSEnabled determines if the websocket server is enabled.
	WSEnabled bool
	// EVMNetworkID provides the EVM chain ID.
	EVMNetworkID *big.Int
	// FlowNetworkID is the Flow network ID that the EVM is hosted on (mainnet, testnet, emulator...)
	FlowNetworkID flowGo.ChainID
	// Coinbase is EVM address that collects the EVM operator fees collected
	// when transactions are being submitted.
	Coinbase common.Address
	// COAAddress is Flow address that holds COA account used for submitting transactions.
	COAAddress flow.Address
	// COAKey is Flow key to the COA account. WARNING: do not use in production
	COAKey crypto.PrivateKey
	// COAKeys is a slice of all the keys that will be used in key-rotation mechanism.
	COAKeys []crypto.PrivateKey
	// COACloudKMSKeys is a slice of all the keys and their versions that will be used in Cloud KMS key-rotation mechanism.
	COACloudKMSKeys []flowGoKMS.Key
	// CreateCOAResource indicates if the COA resource should be auto-created on
	// startup if one doesn't exist in the COA Flow address account
	CreateCOAResource bool
	// GasPrice is a fixed gas price that will be used when submitting transactions.
	GasPrice *big.Int
	// InitCadenceHeight is used for initializing the database on a local emulator or a live network.
	InitCadenceHeight uint64
	// LogLevel defines how verbose the output log is
	LogLevel zerolog.Level
	// LogWriter defines the writer used for logging
	LogWriter io.Writer
	// RateLimit requests made by the client identified by IP over any protocol (ws/http).
	RateLimit uint64
	// Address header used to identified clients, usually set by the proxy
	AddressHeader string
	// StreamLimit rate-limits the events sent to the client within 1 second time interval.
	StreamLimit float64
	// StreamTimeout defines the timeout the server waits for the event to be sent to the client.
	StreamTimeout time.Duration
	// FilterExpiry defines the time it takes for an idle filter to expire
	FilterExpiry time.Duration
	// ForceStartCadenceHeight will force set the starting Cadence height, this should be only used for testing or locally.
	ForceStartCadenceHeight uint64
	// HeartbeatInterval sets custom heartbeat interval for events
	HeartbeatInterval uint64
	// TracesBucketName sets the GCP bucket name where transaction traces are being stored.
	TracesBucketName string
	// TracesEnabled sets whether the node is supporting transaction traces.
	TracesEnabled bool
	// WalletEnabled sets whether wallet APIs are enabled
	WalletEnabled bool
	// WalletKey used for signing transactions
	WalletKey *ecdsa.PrivateKey
}

func FromFlags() (*Config, error) {
	cfg := &Config{}
	var (
		evmNetwork,
		coinbase,
		gas,
		coa,
		key,
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

	// parse from flags
	flag.StringVar(&cfg.DatabaseDir, "database-dir", "./db", "Path to the directory for the database")
	flag.StringVar(&cfg.RPCHost, "rpc-host", "", "Host for the RPC API server")
	flag.IntVar(&cfg.RPCPort, "rpc-port", 8545, "Port for the RPC API server")
	flag.BoolVar(&cfg.WSEnabled, "ws-enabled", false, "Enable websocket connections")
	flag.StringVar(&cfg.AccessNodeHost, "access-node-grpc-host", "localhost:3569", "Host to the flow access node gRPC API")
	flag.StringVar(&accessSporkHosts, "access-node-spork-hosts", "", `Previous spork AN hosts, defined following the schema: {host1},{host2} as a comma separated list (e.g. "host-1.com,host2.com")`)
	flag.StringVar(&evmNetwork, "evm-network-id", "previewnet", "EVM network ID (previewnet, testnet, mainnet)")
	flag.StringVar(&flowNetwork, "flow-network-id", "flow-emulator", "Flow network ID (flow-emulator, flow-previewnet, flow-testnet)")
	flag.StringVar(&coinbase, "coinbase", "", "Coinbase address to use for fee collection")
	flag.Uint64Var(&initHeight, "init-cadence-height", 0, "Define the Cadence block height at which to start the indexing, if starting on a new network this flag should not be used.")
	flag.StringVar(&gas, "gas-price", "1", "Static gas price used for EVM transactions")
	flag.StringVar(&coa, "coa-address", "", "Flow address that holds COA account used for submitting transactions")
	flag.StringVar(&key, "coa-key", "", "Private key value for the COA address used for submitting transactions")
	flag.StringVar(&keysPath, "coa-key-file", "", "File path that contains JSON array of COA keys used in key-rotation mechanism, this is exclusive with coa-key flag.")
	flag.BoolVar(&cfg.CreateCOAResource, "coa-resource-create", false, "Auto-create the COA resource in the Flow COA account provided if one doesn't exist")
	flag.StringVar(&logLevel, "log-level", "debug", "Define verbosity of the log output ('debug', 'info', 'warn', 'error', 'fatal', 'panic')")
	flag.StringVar(&logWriter, "log-writer", "stderr", "Log writer used for output ('stderr', 'console')")
	flag.Float64Var(&cfg.StreamLimit, "stream-limit", 10, "Rate-limits the events sent to the client within one second")
	flag.Uint64Var(&cfg.RateLimit, "rate-limit", 50, "Rate-limit requests per second made by the client over any protocol (ws/http)")
	flag.StringVar(&cfg.AddressHeader, "address-header", "", "Address header that contains the client IP, this is useful when the server is behind a proxy that sets the source IP of the client. Leave empty if no proxy is used.")
	flag.Uint64Var(&cfg.HeartbeatInterval, "heartbeat-interval", 100, "Heartbeat interval for AN event subscription")
	flag.IntVar(&streamTimeout, "stream-timeout", 3, "Defines the timeout in seconds the server waits for the event to be sent to the client")
	flag.Uint64Var(&forceStartHeight, "force-start-height", 0, "Force set starting Cadence height. WARNING: This should only be used locally or for testing, never in production.")
	flag.StringVar(&filterExpiry, "filter-expiry", "5m", "Filter defines the time it takes for an idle filter to expire")
	flag.StringVar(&cfg.TracesBucketName, "traces-gcp-bucket", "", "GCP bucket name where transaction traces are stored")
	flag.StringVar(&cloudKMSProjectID, "coa-cloud-kms-project-id", "", "The project ID containing the KMS keys, e.g. 'flow-evm-gateway'")
	flag.StringVar(&cloudKMSLocationID, "coa-cloud-kms-location-id", "", "The location ID where the key ring is grouped into, e.g. 'global'")
	flag.StringVar(&cloudKMSKeyRingID, "coa-cloud-kms-key-ring-id", "", "The key ring ID where the KMS keys exist, e.g. 'tx-signing'")
	flag.StringVar(&cloudKMSKeys, "coa-cloud-kms-keys", "", `Names of the KMS keys and their versions as a comma separated list, e.g. "gw-key-6@1,gw-key-7@1,gw-key-8@1"`)
	flag.StringVar(&walletKey, "wallet-api-key", "", "ECDSA private key used for wallet APIs. WARNING: This should only be used locally or for testing, never in production.")
	flag.Parse()

	if coinbase == "" {
		return nil, fmt.Errorf("coinbase EVM address required")
	}
	cfg.Coinbase = common.HexToAddress(coinbase)
	if g, ok := new(big.Int).SetString(gas, 10); ok {
		cfg.GasPrice = g
	}

	cfg.COAAddress = flow.HexToAddress(coa)
	if cfg.COAAddress == flow.EmptyAddress {
		return nil, fmt.Errorf("invalid COA address value")
	}

	if key != "" {
		pkey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, key)
		if err != nil {
			return nil, fmt.Errorf("invalid COA key: %w", err)
		}
		cfg.COAKey = pkey
	} else if keysPath != "" {
		raw, err := os.ReadFile(keysPath)
		if err != nil {
			return nil, fmt.Errorf("could not read the file containing list of keys for key-rotation mechanism, check if coa-key-file specifies valid path: %w", err)
		}
		var keysJSON []string
		if err := json.Unmarshal(raw, &keysJSON); err != nil {
			return nil, fmt.Errorf("could not parse file containing the list of keys for key-rotation, make sure keys are in JSON array format: %w", err)
		}

		cfg.COAKeys = make([]crypto.PrivateKey, len(keysJSON))
		for i, k := range keysJSON {
			pk, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, k) // todo support different algos
			if err != nil {
				return nil, fmt.Errorf("a key from the COA key list file is not valid, key %s, error: %w", k, err)
			}
			cfg.COAKeys[i] = pk
		}
	} else if cloudKMSKeys != "" {
		if cloudKMSProjectID == "" || cloudKMSLocationID == "" || cloudKMSKeyRingID == "" {
			return nil, fmt.Errorf(
				"using coa-cloud-kms-keys requires also coa-cloud-kms-project-id & coa-cloud-kms-location-id & coa-cloud-kms-key-ring-id",
			)
		}

		kmsKeys := strings.Split(cloudKMSKeys, ",")
		cfg.COACloudKMSKeys = make([]flowGoKMS.Key, len(kmsKeys))
		for i, key := range kmsKeys {
			// key has the form "{keyID}@{keyVersion}"
			keyParts := strings.Split(key, "@")
			if len(keyParts) != 2 {
				return nil, fmt.Errorf("wrong format for Cloud KMS key: %s", key)
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
		return nil, fmt.Errorf(
			"must either provide coa-key / coa-key-path / coa-cloud-kms-keys",
		)
	}

	switch evmNetwork {
	case "previewnet":
		cfg.EVMNetworkID = types.FlowEVMPreviewNetChainID
	case "testnet":
		cfg.EVMNetworkID = types.FlowEVMTestNetChainID
	case "mainnet":
		cfg.EVMNetworkID = types.FlowEVMMainNetChainID
	default:
		return nil, fmt.Errorf("EVM network ID not supported")
	}

	switch flowNetwork {
	case "flow-previewnet":
		cfg.FlowNetworkID = flowGo.Previewnet
		cfg.InitCadenceHeight = LiveNetworkInitCadenceHeght
	case "flow-emulator":
		cfg.FlowNetworkID = flowGo.Emulator
		cfg.InitCadenceHeight = EmulatorInitCadenceHeight
	case "flow-testnet":
		cfg.FlowNetworkID = flowGo.Testnet
		cfg.InitCadenceHeight = LiveNetworkInitCadenceHeght
	default:
		return nil, fmt.Errorf("flow network ID not supported, only possible to specify 'flow-previewnet' or 'flow-emulator'")
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
	}

	if logWriter == "stderr" {
		cfg.LogWriter = os.Stderr
	} else {
		cfg.LogWriter = zerolog.NewConsoleWriter()
	}

	cfg.StreamTimeout = time.Second * time.Duration(streamTimeout)

	exp, err := time.ParseDuration(filterExpiry)
	if err != nil {
		return nil, fmt.Errorf("filter expiry not valid unit: %w", err)
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

	if walletKey != "" {
		var k, err = gethCrypto.HexToECDSA(walletKey)
		if err != nil {
			return nil, fmt.Errorf("wrong private key for wallet API: %w", err)
		}

		cfg.WalletKey = k
		cfg.WalletEnabled = true
	}

	// todo validate Config values
	return cfg, nil
}
