package config

import (
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"math/big"
)

const (
	EmptyHeight = math.MaxUint64
	// todo probably not good idea to have a default for this
	defaultCoinbase = "0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"
)

type Config struct {
	// DatabaseDir is where the database should be stored.
	DatabaseDir string
	// AccessNodeGRPCHost defines the Flow network AN host.
	AccessNodeGRPCHost string
	// todo support also just specifying latest height
	// InitHeight provides initial heights for EVM block height
	// useful only on a cold-start with an empty database, otherwise
	// should be avoided or an error will be thrown.
	InitHeight uint64
	// ChainID provides the EVM chain ID.
	ChainID *big.Int
	// Coinbase is EVM address that collects the EVM operator fees collected
	// when transactions are being submitted.
	Coinbase common.Address
	// COAAddress is Flow address that holds COA account used for submitting transactions.
	COAAddress flow.Address
	// GasPrice is a fixed gas price that will be used when submitting transactions.
	GasPrice *big.Int
}

func FromFlags() (*Config, error) {
	cfg := &Config{}
	var network, coinbase, gas, coa string

	// parse from flags
	flag.StringVar(&cfg.DatabaseDir, "database-dir", "./db", "path to the directory for the database")
	flag.StringVar(&cfg.AccessNodeGRPCHost, "access-node-grpc-host", "localhost:3569", "host to the flow access node gRPC API")
	flag.Uint64Var(&cfg.InitHeight, "init-height", EmptyHeight, "init cadence block height from where the event ingestion will start. WARNING: you should only provide this if there are no existing values in the database, otherwise an error will be thrown")
	flag.StringVar(&network, "network-id", "testnet", "EVM network ID (testnet, mainnet)")
	flag.StringVar(&coinbase, "coinbase", defaultCoinbase, "coinbase address to use for fee collection")
	flag.StringVar(&gas, "gas-price", "1", "static gas price used for EVM transactions")
	flag.StringVar(&coa, "coa-address", "", "Flow address that holds COA account used for submitting transactions")
	flag.Parse()

	cfg.Coinbase = common.HexToAddress(coinbase)
	if g, ok := new(big.Int).SetString(gas, 10); ok {
		cfg.GasPrice = g
	}

	cfg.COAAddress = flow.HexToAddress(coa)
	if cfg.COAAddress == flow.EmptyAddress {
		return nil, fmt.Errorf("invalid COA address value")
	}

	switch network {
	case "testnet":
		cfg.ChainID = emulator.FlowEVMTestnetChainID
	case "mainnet":
		cfg.ChainID = emulator.FlowEVMMainnetChainID
	default:
		return nil, fmt.Errorf("network ID not supported")
	}

	// todo validate Config values
	return cfg, nil
}
