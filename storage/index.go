package storage

import (
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"

	"github.com/onflow/flow-evm-gateway/models"
)

type BlockIndexer interface {
	// Store provided EVM block with the matching Cadence height and Cadence Block ID.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	// Expected errors:
	// - errors.Duplicate if the block already exists
	Store(cadenceHeight uint64, cadenceID flow.Identifier, block *models.Block, batch *pebble.Batch) error

	// GetByHeight returns an EVM block stored by EVM height.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetByHeight(height uint64) (*models.Block, error)

	// GetByID returns an EVM block stored by ID.
	// Expected errors:
	// - errors.NotFound if the block is not found
	GetByID(ID common.Hash) (*models.Block, error)

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
	SetLatestCadenceHeight(cadenceHeight uint64, batch *pebble.Batch) error

	// GetCadenceHeight returns the Cadence height that matches the
	// provided EVM height. Each EVM block indexed contains a link
	// to the Cadence height.
	// - errors.NotFound if the height is not found
	GetCadenceHeight(height uint64) (uint64, error)

	// GetCadenceID returns the Cadence block ID that matches the
	// provided EVM height. Each EVM block indexed contains a link to the
	// Cadence block ID. Multiple EVM heights can point to the same
	// Cadence block ID.
	// - errors.NotFound if the height is not found
	GetCadenceID(height uint64) (flow.Identifier, error)
}

type ReceiptIndexer interface {
	// Store provided receipt.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	Store(receipts []*models.Receipt, batch *pebble.Batch) error

	// GetByTransactionID returns the receipt for the transaction ID.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	GetByTransactionID(ID common.Hash) (*models.Receipt, error)

	// GetByBlockHeight returns the receipt for the block height.
	// Expected errors:
	// - errors.NotFound if the receipt is not found
	GetByBlockHeight(height uint64) ([]*models.Receipt, error)

	// BloomsForBlockRange returns slice of bloom values and a slice of block heights
	// corresponding to each item in the bloom slice. It only matches the blooms between
	// inclusive start and end block height.
	// Expected errors:
	// - errors.InvalidRange if the block by the height was not indexed or if the end and start values are invalid.
	BloomsForBlockRange(start, end uint64) ([]*models.BloomsHeight, error)
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

type TraceIndexer interface {
	// StoreTransaction will index transaction trace by the transaction ID.
	// Batch is required to batch multiple indexer operations, skipped if nil.
	StoreTransaction(ID common.Hash, trace json.RawMessage, batch *pebble.Batch) error

	// GetTransaction will retrieve transaction trace by the transaction ID.
	GetTransaction(ID common.Hash) (json.RawMessage, error)
}

// UserOperationIndexer indexes UserOperation events and receipts
type UserOperationIndexer interface {
	// StoreUserOpReceipt stores a UserOperation receipt
	StoreUserOpReceipt(userOpHash common.Hash, receipt *UserOperationReceipt, batch *pebble.Batch) error
	// GetUserOpReceipt retrieves a UserOperation receipt by hash
	GetUserOpReceipt(userOpHash common.Hash) (*UserOperationReceipt, error)
	// StoreUserOpTxMapping stores the mapping from userOpHash to transaction hash
	StoreUserOpTxMapping(userOpHash common.Hash, txHash common.Hash, batch *pebble.Batch) error
	// GetTxHashByUserOpHash retrieves the transaction hash for a UserOperation
	GetTxHashByUserOpHash(userOpHash common.Hash) (common.Hash, error)
}

// UserOperationReceipt represents a receipt for a UserOperation execution
type UserOperationReceipt struct {
	UserOpHash    common.Hash
	EntryPoint    common.Address
	Sender        common.Address
	Nonce         *big.Int
	Paymaster     *common.Address
	ActualGasCost *big.Int
	ActualGasUsed *big.Int
	Success       bool
	Reason        string
	Logs          []interface{}
	TxHash        common.Hash
	BlockNumber   *big.Int
	BlockHash     common.Hash
}
