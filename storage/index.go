package storage

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
)

type BlockIndexer interface {
	Store(block *types.Block) error
	GetByHeight(height uint64) (*types.Block, error)
	GetByID(ID common.Hash) (*types.Block, error)
	LatestHeight() (uint64, error)
	FirstHeight() (uint64, error)
}

type ReceiptIndexer interface {
	Store(receipt *gethTypes.ReceiptForStorage) error
	GetByTransactionID(ID common.Hash) (*gethTypes.ReceiptForStorage, error)
	GetByBlockID(ID common.Hash) (*gethTypes.ReceiptForStorage, error)
	BloomsForBlockRange(start, end uint64) []*gethTypes.Bloom
}

type TransactionIndexer interface {
	Store(tx *gethTypes.Transaction) error
	Get(ID common.Hash) (*gethTypes.Transaction, error)
}
