package storage

import (
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
)

type BlockIndex interface {
	Store(block *types.Block, height uint64) error
	Get(height uint64) (*types.Block, error)
	LatestHeight() (uint64, error)
	FirstHeight() (uint64, error)
}

type LogsIndex interface {
	Store(logs []*gethTypes.Log) error
	Get(topic string) ([]*gethTypes.Log, error)
}
