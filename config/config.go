package config

import (
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
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
	// AccessNodePreviousSporkHosts contains a map of latest heights for each spork,
	// which can be accessed via the host of the AN provided
	AccessNodePreviousSporkHosts map[uint64]string
	// GRPCPort for the RPC API server
	RPCPort int
	// GRPCHost for the RPC API server
	RPCHost string
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
	// StreamLimit rate-limits the events sent to the client within 1 second time interval.
	StreamLimit float64
	// StreamTimeout defines the timeout the server waits for the event to be sent to the client.
	StreamTimeout time.Duration
	// FilterExpiry defines the time it takes for an idle filter to expire
	FilterExpiry time.Duration
}

func FromFlags() (*Config, error) {
	cfg := &Config{
		AccessNodePreviousSporkHosts: make(map[uint64]string),
	}
	var evmNetwork, coinbase, gas, coa, key, keysPath, flowNetwork, logLevel, filterExpiry, accessSporkHosts string
	var streamTimeout int
	var initHeight uint64

	// parse from flags
	flag.StringVar(&cfg.DatabaseDir, "database-dir", "./db", "Path to the directory for the database")
	flag.StringVar(&cfg.RPCHost, "rpc-host", "", "Host for the RPC API server")
	flag.IntVar(&cfg.RPCPort, "rpc-port", 8545, "Port for the RPC API server")
	flag.StringVar(&cfg.AccessNodeHost, "access-node-grpc-host", "localhost:3569", "Host to the flow access node gRPC API")
	flag.StringVar(&accessSporkHosts, "access-node-spork-hosts", "", `Previous spork AN hosts, defined following the schema: {latest height}@{host} as comma separated list (e.g. "200@host-1.com,300@host2.com")`)
	flag.StringVar(&evmNetwork, "evm-network-id", "previewnet", "EVM network ID (previewnet, testnet, mainnet)")
	flag.StringVar(&flowNetwork, "flow-network-id", "flow-emulator", "Flow network ID (flow-emulator, flow-previewnet)")
	flag.StringVar(&coinbase, "coinbase", "", "Coinbase address to use for fee collection")
	flag.Uint64Var(&initHeight, "init-cadence-height", 0, "Define the Cadence block height at which to start the indexing, if starting on a new network this flag should not be used.")
	flag.StringVar(&gas, "gas-price", "1", "Static gas price used for EVM transactions")
	flag.StringVar(&coa, "coa-address", "", "Flow address that holds COA account used for submitting transactions")
	flag.StringVar(&key, "coa-key", "", "Private key value for the COA address used for submitting transactions")
	flag.StringVar(&keysPath, "coa-key-file", "", "File path that contains JSON array of COA keys used in key-rotation mechanism, this is exclusive with coa-key flag.")
	flag.BoolVar(&cfg.CreateCOAResource, "coa-resource-create", false, "Auto-create the COA resource in the Flow COA account provided if one doesn't exist")
	flag.StringVar(&logLevel, "log-level", "debug", "Define verbosity of the log output ('debug', 'info', 'error')")
	flag.Float64Var(&cfg.StreamLimit, "stream-limit", 10, "Rate-limits the events sent to the client within one second")
	flag.IntVar(&streamTimeout, "stream-timeout", 3, "Defines the timeout in seconds the server waits for the event to be sent to the client")
	flag.StringVar(&filterExpiry, "filter-expiry", "5m", "Filter defines the time it takes for an idle filter to expire")
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
	} else {
		return nil, fmt.Errorf("must either provide coa-key or coa-key-path flag")
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
	case "error":
		cfg.LogLevel = zerolog.ErrorLevel
	}

	cfg.LogWriter = os.Stdout

	cfg.StreamTimeout = time.Second * time.Duration(streamTimeout)

	exp, err := time.ParseDuration(filterExpiry)
	if err != nil {
		return nil, fmt.Errorf("filter expiry not valid unit: %w", err)
	}
	cfg.FilterExpiry = exp

	if accessSporkHosts != "" {
		heightHosts := strings.Split(accessSporkHosts, ",")
		for _, hh := range heightHosts {
			v := strings.Split(hh, "@")
			heightVal, host := v[0], v[1]
			height, err := strconv.Atoi(heightVal)
			if err != nil {
				return nil, fmt.Errorf("failed to parse AN host height value for previous sporks, provided with --access-node-spork-hosts flag")
			}

			cfg.AccessNodePreviousSporkHosts[uint64(height)] = host
		}
	}

	// todo validate Config values
	return cfg, nil
}
