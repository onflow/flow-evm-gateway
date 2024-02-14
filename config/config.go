package config

import (
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"math/big"
)

const (
	EmptyHeight = math.MaxUint64
	// todo probably not good idea to have a default for this
	defaultCoinbase = "0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"
)

type Config struct {
	DatabaseDir        string
	AccessNodeGRPCHost string
	// todo support also just specifying latest height
	InitHeight uint64
	ChainID    *big.Int
	Coinbase   common.Address
}

func FromFlags() (*Config, error) {
	cfg := &Config{}
	var network, coinbase string

	// parse from flags
	flag.StringVar(&cfg.DatabaseDir, "database-dir", "./db", "path to the directory for the database")
	flag.StringVar(&cfg.AccessNodeGRPCHost, "access-node-grpc-host", "localhost:3569", "host to the flow access node gRPC API")
	flag.Uint64Var(&cfg.InitHeight, "init-height", EmptyHeight, "init cadence block height from where the event ingestion will start. WARNING: you should only provide this if there are no existing values in the database, otherwise an error will be thrown")
	flag.StringVar(&network, "network ID", "emulator", "EVM network ID (testnet, mainnet)")
	flag.StringVar(&coinbase, "coinbase", defaultCoinbase, "coinbase address to use for fee collection")
	flag.Parse()

	cfg.Coinbase = common.HexToAddress(coinbase)

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
