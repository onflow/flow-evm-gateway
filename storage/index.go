package storage

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
)

type BlockIndexer interface {
	// Store provided block.
	// Expected errors:
	// - errors.Duplicate if the block already exists
	Store(block *types.Block) error

	// GetByHeight returns a block stored by height.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetByHeight(height uint64) (*types.Block, error)

	// GetByID returns a block stored by ID.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetByID(ID common.Hash) (*types.Block, error)

	// LatestHeight returns the latest stored block height.
	// Expected errors:
	// - errors.NotInitialized if the storage was not initialized
	LatestHeight() (uint64, error)

	// FirstHeight returns the first stored block height.
	// Expected errors:
	// - errors.NotInitialized if the storage was not initialized
	FirstHeight() (uint64, error)
}

type ReceiptIndexer interface {
	// Store provided receipt.
	// Expected errors:
	// - errors.Duplicate if the block already exists.
	Store(receipt *gethTypes.ReceiptForStorage) error

	// GetByTransactionID returns the receipt for the transaction ID.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	GetByTransactionID(ID common.Hash) (*gethTypes.ReceiptForStorage, error)

	// GetByBlockID returns the receipt for the block ID.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	// TODO right now one transaction per block, but this might change in future so the API needs to be updated.
	GetByBlockID(ID common.Hash) (*gethTypes.ReceiptForStorage, error)

	// BloomsForBlockRange returns bloom values for the block height range.
	// Goes through all the blocks in the range if the range is valid and returns the bloom values.
	// Expected errors:
	// - errors.InvalidRange if the provided height range is invalid.
	BloomsForBlockRange(start, end uint64) ([]*gethTypes.Bloom, error)
}

type TransactionIndexer interface {
	// Store provided transaction.
	// Expected errors:
	// - errors.Duplicate if the transaction with the ID already exists.
	Store(tx *gethTypes.Transaction) error

	// Get transaction by the ID.
	// Expected errors:
	// - errors.NotFound if the transaction with the ID is not found.
	Get(ID common.Hash) (*gethTypes.Transaction, error)
}
