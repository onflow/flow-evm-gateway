package api

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type Config struct {
	ChainID  *big.Int
	Coinbase common.Address
}
