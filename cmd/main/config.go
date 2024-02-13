package main

import (
	"flag"
	"github.com/ethereum/go-ethereum/common/math"
)

const emptyHeight = math.MaxUint64

type config struct {
	databaseDir        string
	accessNodeGRPCHost string
	// todo support also just specifying latest height
	initHeight uint64
}

func configFromFlags() (*config, error) {
	cfg := &config{}

	// parse from flags
	flag.StringVar(&cfg.databaseDir, "database-dir", "./db", "path to the directory for the database")
	flag.StringVar(&cfg.accessNodeGRPCHost, "access-node-grpc-host", "localhost:3569", "host to the flow access node gRPC API")
	flag.Uint64Var(&cfg.initHeight, "init-height", emptyHeight, "init cadence block height from where the event ingestion will start. WARNING: you should only provide this if there are no existing values in the database, otherwise an error will be thrown")
	flag.Parse()

	// todo validate config values
	return cfg, nil
}
