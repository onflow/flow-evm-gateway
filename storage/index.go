package storage

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
	"math/big"
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
	Store(receipt *gethTypes.Receipt) error

	// GetByTransactionID returns the receipt for the transaction ID.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	GetByTransactionID(ID common.Hash) (*gethTypes.Receipt, error)

	// GetByBlockHeight returns the receipt for the block height.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	// TODO right now one transaction per block, but this might change in future so the API needs to be updated.
	GetByBlockHeight(height *big.Int) (*gethTypes.Receipt, error)

	// BloomsForBlockRange returns slice of bloom values and a slice of block heights
	// corresponding to each item in the bloom slice. It only matches the blooms between
	// inclusive start and end block height.
	// Expected errors:
	// - errors.InvalidRange if the block by the height was not indexed or if the end and start values are invalid.
	BloomsForBlockRange(start, end *big.Int) ([]*gethTypes.Bloom, []*big.Int, error)
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

type AccountIndexer interface {
	// Update account with executed transactions.
	Update(tx *gethTypes.Transaction) error

	// GetNonce gets an account nonce. If no nonce was indexed it returns 0.
	// todo add getting nonce at provided block height / hash
	GetNonce(address *common.Address) (uint64, error)

	// GetBalance gets an account balance. If no balance was indexer it returns 0.
	GetBalance(address *common.Address) (*big.Int, error)
}
