package memory

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-go/fvm/evm/types"
	"math/big"
	"sync"
)

const (
	unknownHeight = math.MaxUint64 - 1
)

type baseStorage struct {
	mu sync.RWMutex

	blocksIDs       map[common.Hash]*types.Block
	blockHeightsIDs map[uint64]common.Hash
	firstHeight     uint64
	lastHeight      uint64

	receiptsTxIDs          map[common.Hash]*gethTypes.Receipt
	receiptBlockHeightTxID map[uint64]common.Hash
	bloomHeight            map[uint64]gethTypes.Bloom

	transactionsIDs map[common.Hash]*gethTypes.Transaction
}

var store *baseStorage

func baseStorageFactory() *baseStorage {
	if store == nil {
		store = &baseStorage{
			blocksIDs:              make(map[common.Hash]*types.Block),
			blockHeightsIDs:        make(map[uint64]common.Hash),
			firstHeight:            unknownHeight,
			lastHeight:             unknownHeight,
			receiptsTxIDs:          make(map[common.Hash]*gethTypes.Receipt),
			receiptBlockHeightTxID: make(map[uint64]common.Hash),
			bloomHeight:            make(map[uint64]gethTypes.Bloom),
			transactionsIDs:        make(map[common.Hash]*gethTypes.Transaction),
		}
	}
	return store
}

var _ storage.BlockIndexer = &BlockStorage{}

type BlockStorage struct {
	base *baseStorage
}

func NewBlockStorage() *BlockStorage {
	return &BlockStorage{
		base: baseStorageFactory(),
	}
}

func (s BlockStorage) GetByHeight(height uint64) (*types.Block, error) {
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()

	// Check if the requested height is within the known range
	if height < s.base.firstHeight || height > s.base.lastHeight {
		return nil, errors.NotFound
	}

	// Retrieve the block using the blockHeightsIDs map
	blockID, exists := s.base.blockHeightsIDs[height]
	if !exists {
		return nil, errors.NotFound
	}

	// Retrieve the block using the blocksIDs map
	block, exists := s.base.blocksIDs[blockID]
	if !exists {
		return nil, errors.NotFound
	}

	return block, nil
}

func (s BlockStorage) GetByID(ID common.Hash) (*types.Block, error) {
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()

	// Retrieve the block using the blocksIDs map
	block, exists := s.base.blocksIDs[ID]
	if !exists {
		return nil, errors.NotFound
	}

	return block, nil
}

func (s BlockStorage) Store(block *types.Block) error {
	s.base.mu.Lock()
	defer s.base.mu.Unlock()

	ID, err := block.Hash()
	if err != nil {
		return fmt.Errorf("block hash error: %w", err)
	}

	// Check if the block already exists
	_, exists := s.base.blocksIDs[ID]
	if exists {
		return errors.Duplicate
	}

	// Store the block in blocksIDs map and update blockHeightsIDs map
	s.base.blocksIDs[ID] = block
	s.base.blockHeightsIDs[block.Height] = ID

	// Update firstHeight and lastHeight if necessary
	if s.base.firstHeight == unknownHeight {
		s.base.firstHeight = block.Height
	}
	if s.base.lastHeight == unknownHeight || block.Height > s.base.lastHeight {
		s.base.lastHeight = block.Height
	}

	return nil
}

func (s BlockStorage) LatestHeight() (uint64, error) {
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()

	if s.base.lastHeight == unknownHeight {
		return 0, errors.NotInitialized
	}
	return s.base.lastHeight, nil
}

func (s BlockStorage) FirstHeight() (uint64, error) {
	s.base.mu.RLock()
	defer s.base.mu.RUnlock()

	if s.base.firstHeight == unknownHeight {
		return 0, errors.NotInitialized
	}
	return s.base.firstHeight, nil
}

var _ storage.ReceiptIndexer = &ReceiptStorage{}

type ReceiptStorage struct {
	base *baseStorage
}

func NewReceiptStorage() *ReceiptStorage {
	return &ReceiptStorage{
		base: baseStorageFactory(),
	}
}

func (r ReceiptStorage) Store(receipt *gethTypes.Receipt) error {
	r.base.mu.Lock()
	defer r.base.mu.Unlock()

	if _, ok := r.base.receiptsTxIDs[receipt.TxHash]; ok {
		return errors.Duplicate
	}

	r.base.receiptsTxIDs[receipt.TxHash] = receipt
	r.base.receiptBlockHeightTxID[receipt.BlockNumber.Uint64()] = receipt.TxHash
	r.base.bloomHeight[receipt.BlockNumber.Uint64()] = receipt.Bloom

	return nil
}

func (r ReceiptStorage) GetByTransactionID(ID common.Hash) (*gethTypes.Receipt, error) {
	r.base.mu.RLock()
	defer r.base.mu.RUnlock()

	receipt, exists := r.base.receiptsTxIDs[ID]
	if !exists {
		return nil, errors.NotFound
	}

	return receipt, nil
}

func (r ReceiptStorage) GetByBlockHeight(height *big.Int) (*gethTypes.Receipt, error) {
	r.base.mu.RLock()
	defer r.base.mu.RUnlock()

	txID, exists := r.base.receiptBlockHeightTxID[height.Uint64()]
	if !exists {
		return nil, errors.NotFound
	}

	receipt, exists := r.base.receiptsTxIDs[txID]
	if !exists {
		return nil, errors.NotFound
	}

	return receipt, nil
}

func (r ReceiptStorage) BloomsForBlockRange(start, end *big.Int) (map[*big.Int]gethTypes.Bloom, error) {
	r.base.mu.RLock()
	defer r.base.mu.RUnlock()

	// make sure start is not bigger than end
	if start.Cmp(end) > 0 {
		return nil, errors.InvalidRange
	}

	blooms := make(map[*big.Int]gethTypes.Bloom)

	// Iterate through the range of block heights and add the blooms to the result
	for i := start.Uint64(); i <= end.Uint64(); i++ {
		b, exists := r.base.bloomHeight[i]
		if !exists {
			return nil, fmt.Errorf("bloom by height not found") // this should not happen
		}
		blooms[big.NewInt(int64(i))] = b
	}

	return blooms, nil
}

var _ storage.TransactionIndexer = &TransactionStorage{}

type TransactionStorage struct {
	base *baseStorage
}

func NewTransactionStorage() *TransactionStorage {
	return &TransactionStorage{
		base: baseStorageFactory(),
	}
}

func (t TransactionStorage) Store(tx *gethTypes.Transaction) error {
	t.base.mu.Lock()
	defer t.base.mu.Unlock()

	_, exists := t.base.transactionsIDs[tx.Hash()]
	if exists {
		return errors.Duplicate
	}

	t.base.transactionsIDs[tx.Hash()] = tx
	return nil
}

func (t TransactionStorage) Get(ID common.Hash) (*gethTypes.Transaction, error) {
	t.base.mu.RLock()
	defer t.base.mu.RUnlock()

	tx, exists := t.base.transactionsIDs[ID]
	if !exists {
		return nil, errors.NotFound
	}

	return tx, nil
}
