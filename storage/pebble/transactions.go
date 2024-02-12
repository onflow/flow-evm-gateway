package pebble

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	"sync"
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

func (t *Transactions) Store(tx *gethTypes.Transaction) error {
	t.mux.Lock()
	defer t.mux.Unlock()

	val, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	return t.store.set(txIDKey, tx.Hash().Bytes(), val)
}

func (t *Transactions) Get(ID common.Hash) (*gethTypes.Transaction, error) {
	t.mux.RLock()
	defer t.mux.RUnlock()

	val, err := t.store.get(txIDKey, ID.Bytes())
	if err != nil {
		return nil, err
	}

	var tx *gethTypes.Transaction
	return tx, tx.UnmarshalBinary(val)
}
