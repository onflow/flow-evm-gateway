package api

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// TODO(m-Peter) Add more config options, such as:
// - host
// - port
// - access URL to connect the indexer to
// - whether JSON-RPC is exposed HTTP/WebSocket or both
// - some connection timeout options etc
type Config struct {
	ChainID  *big.Int
	Coinbase common.Address
}
