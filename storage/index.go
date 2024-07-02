package storage

import (
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"

	"github.com/onflow/flow-evm-gateway/models"
)

type BlockIndexer interface {
	// Store provided EVM block with the matching Cadence height and Cadence Block ID.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	// Expected errors:
	// - errors.Duplicate if the block already exists
	Store(cadenceHeight uint64, cadenceID flow.Identifier, block *types.Block, batch *pebble.Batch) error

	// GetByHeight returns an EVM block stored by EVM height.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetByHeight(height uint64) (*types.Block, error)

	// GetByID returns an EVM block stored by ID.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetByID(ID common.Hash) (*types.Block, error)

	// GetHeightByID returns the EVM block height for the given ID.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetHeightByID(ID common.Hash) (uint64, error)

	// LatestEVMHeight returns the latest stored EVM block height.
	// Expected errors:
	// - errors.NotInitialized if the storage was not initialized
	LatestEVMHeight() (uint64, error)

	// LatestCadenceHeight return the latest stored Cadence height.
	// Expected errors:
	// - errors.NotInitialized if the storage was not initialized
	LatestCadenceHeight() (uint64, error)

	// SetLatestCadenceHeight sets the latest Cadence height.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	SetLatestCadenceHeight(height uint64, batch *pebble.Batch) error

	// GetCadenceHeight returns the Cadence height that matches the
	// provided EVM height. Each EVM block indexed contains a link
	// to the Cadence height.
	// - errors.NotFound if the height is not found
	GetCadenceHeight(evmHeight uint64) (uint64, error)

	// GetCadenceID returns the Cadence block ID that matches the
	// provided EVM height. Each EVM block indexed contains a link to the
	// Cadence block ID. Multiple EVM heights can point to the same
	// Cadence block ID.
	// - errors.NotFound if the height is not found
	GetCadenceID(evmHeight uint64) (flow.Identifier, error)
}

type ReceiptIndexer interface {
	// Store provided receipt.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	// Expected errors:
	// - errors.Duplicate if the block already exists.
	Store(receipt *models.StorageReceipt, batch *pebble.Batch) error

	// GetByTransactionID returns the receipt for the transaction ID.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	GetByTransactionID(ID common.Hash) (*models.StorageReceipt, error)

	// GetByBlockHeight returns the receipt for the block height.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	GetByBlockHeight(height *big.Int) ([]*models.StorageReceipt, error)

	// BloomsForBlockRange returns slice of bloom values and a slice of block heights
	// corresponding to each item in the bloom slice. It only matches the blooms between
	// inclusive start and end block height.
	// Expected errors:
	// - errors.InvalidRange if the block by the height was not indexed or if the end and start values are invalid.
	BloomsForBlockRange(start, end *big.Int) ([]*gethTypes.Bloom, []*big.Int, error)
}

type TransactionIndexer interface {
	// Store provided transaction.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	// Expected errors:
	// - errors.Duplicate if the transaction with the ID already exists.
	Store(tx models.Transaction, batch *pebble.Batch) error

	// Get transaction by the ID.
	// Expected errors:
	// - errors.NotFound if the transaction with the ID is not found.
	Get(ID common.Hash) (models.Transaction, error)
}

type AccountIndexer interface {
	// Update account with executed transactions.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	Update(tx models.Transaction, receipt *models.StorageReceipt, batch *pebble.Batch) error

	// GetNonce gets an account nonce. If no nonce was indexed it returns 0.
	// todo add getting nonce at provided block height / hash
	GetNonce(address common.Address) (uint64, error)

	// GetBalance gets an account balance. If no balance was indexed it returns 0.
	GetBalance(address common.Address) (*big.Int, error)
}

type TraceIndexer interface {
	// StoreTransaction will index transaction trace by the transaction ID.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	StoreTransaction(ID common.Hash, trace json.RawMessage, batch *pebble.Batch) error

	// GetTransaction will retrieve transaction trace by the transaction ID.
	GetTransaction(ID common.Hash) (json.RawMessage, error)
}
