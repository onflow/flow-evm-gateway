package pebble

import (
	"encoding/binary"
	"sync"

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

func (t *Transactions) Store(tx models.Transaction) error {
	t.mux.Lock()
	defer t.mux.Unlock()

	val, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	txHash := tx.Hash()

	return t.store.set(txIDKey, txHash.Bytes(), val, nil)
}

func (t *Transactions) Get(ID common.Hash) (models.Transaction, error) {
	t.mux.RLock()
	defer t.mux.RUnlock()

	val, err := t.store.get(txIDKey, ID.Bytes())
	if err != nil {
		return nil, err
	}

	// TEMP: Remove this after PreviewNet is reset.
	// Needed only for backwards compatibility with the
	// direct call hash calculation breaking change.
	heightVal, err := t.store.get(latestCadenceHeightKey)
	if err != nil {
		heightVal = []byte{0, 0, 0, 0, 0, 0, 0, 0}
	}

	cadenceHeight := binary.BigEndian.Uint64(heightVal)

	return models.UnmarshalTransaction(val, cadenceHeight)
}
