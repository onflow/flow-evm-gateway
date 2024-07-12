package pebble

import (
	"math/big"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.TransactionIndexer = &Transactions{}

type Transactions struct {
	store *Storage
	mux   sync.RWMutex
}

func NewTransactions(store *Storage) *Transactions {
	return &Transactions{
		store: store,
		mux:   sync.RWMutex{},
	}
}

func (t *Transactions) Store(tx models.Transaction, batch *pebble.Batch) error {
	t.mux.Lock()
	defer t.mux.Unlock()

	val, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	txHash := tx.Hash()

	return t.store.set(txIDKey, txHash.Bytes(), val, batch)
}

func (t *Transactions) Get(ID common.Hash) (models.Transaction, error) {
	t.mux.RLock()
	defer t.mux.RUnlock()

	val, err := t.store.get(txIDKey, ID.Bytes())
	if err != nil {
		return nil, err
	}

	var evmHeight uint64
	height, err := t.store.get(receiptTxIDToHeightKey, ID.Bytes())
	if err != nil {
		evmHeight = 0
	} else {
		evmHeight = big.NewInt(0).SetBytes(height).Uint64()
	}

	return models.UnmarshalTransaction(val, evmHeight)
}
