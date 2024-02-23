package config

import (
	"flag"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/onflow/flow-go/utils/io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm/emulator"
)

const (
	EmptyHeight = math.MaxUint64
)

type Config struct {
	// DatabaseDir is where the database should be stored.
	DatabaseDir string
	// AccessNodeGRPCHost defines the Flow network AN host.
	AccessNodeGRPCHost string
	// GRPCPort for the RPC API server
	RPCPort int
	// GRPCHost for the RPC API server
	// todo maybe merge port into host as it's for AN
	RPCHost string
	// todo support also just specifying latest height
	// InitCadenceHeight provides initial heights for Cadence block height
	// useful only on a cold-start with an empty database
	InitCadenceHeight uint64
	// EVMNetworkID provides the EVM chain ID.
	EVMNetworkID *big.Int
	// FlowNetworkID is the Flow network ID that the EVM is hosted on (mainnet, testnet, emulator...)
	FlowNetworkID string
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
}

func FromFlags() (*Config, error) {
	cfg := &Config{}
	var evmNetwork, coinbase, gas, coa, key, keysPath string

	// parse from flags
	flag.StringVar(&cfg.DatabaseDir, "database-dir", "./db", "path to the directory for the database")
	flag.StringVar(&cfg.RPCHost, "rpc-host", "", "host for the RPC API server")
	flag.IntVar(&cfg.RPCPort, "rpc-port", 3000, "port for the RPC API server")
	flag.StringVar(&cfg.AccessNodeGRPCHost, "access-node-grpc-host", "localhost:3569", "host to the flow access node gRPC API")
	flag.Uint64Var(&cfg.InitCadenceHeight, "init-cadence-height", EmptyHeight, "init cadence block height from where the event ingestion will start. WARNING: you should only provide this if there are no existing values in the database")
	flag.StringVar(&evmNetwork, "evm-network-id", "testnet", "EVM network ID (testnet, mainnet)")
	flag.StringVar(&cfg.FlowNetworkID, "flow-network-id", "emulator", "EVM network ID (emulator, previewnet)")
	flag.StringVar(&coinbase, "coinbase", "", "coinbase address to use for fee collection")
	flag.StringVar(&gas, "gas-price", "1", "static gas price used for EVM transactions")
	flag.StringVar(&coa, "coa-address", "", "Flow address that holds COA account used for submitting transactions")
	flag.StringVar(&key, "coa-key", "", "WARNING: do not use this flag in production! private key value for the COA address used for submitting transactions")
	flag.StringVar(&keysPath, "coa-key-file", "", "File path that contains JSON array of COA keys used in key-rotation mechanism, this is exclusive with coa-key flag.")
	flag.BoolVar(&cfg.CreateCOAResource, "coa-resource-create", false, "auto-create the COA resource in the Flow COA account provided if one doesn't exist")
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

	pkey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, key)
	if err != nil {
		return nil, fmt.Errorf("invalid COA key: %w", err)
	}
	cfg.COAKey = pkey

	switch evmNetwork {
	case "testnet":
		cfg.EVMNetworkID = emulator.FlowEVMTestnetChainID
	case "mainnet":
		cfg.EVMNetworkID = emulator.FlowEVMMainnetChainID
	default:
		return nil, fmt.Errorf("EVM network ID not supported")
	}

	raw, err := io.ReadFile(keysPath)
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

	if cfg.FlowNetworkID != "previewnet" && cfg.FlowNetworkID != "emulator" {
		return nil, fmt.Errorf("flow network ID is invalid, only allowed to set 'emulator' and 'previewnet'")
	}

	// todo validate Config values
	return cfg, nil
}
