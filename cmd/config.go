package cmd

import "github.com/ethereum/go-ethereum/common/math"

const emptyHeight = math.MaxUint64

type config struct {
	pebbleDBDir        string
	accessNodeGRPCHost string
	// todo support also just specifying latest height
	initHeight uint64
}
