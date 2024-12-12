package pebble

import (
	"github.com/cockroachdb/pebble"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.TransactionIndexer = &Transactions{}

type Transactions struct {
	store *Storage
}

func NewTransactions(store *Storage) *Transactions {
	return &Transactions{
		store: store,
	}
}

func (t *Transactions) Store(tx models.Transaction, batch *pebble.Batch) error {
	val, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	txHash := tx.Hash()

	return t.store.set(txIDKey, txHash.Bytes(), val, batch)
}

func (t *Transactions) Get(ID common.Hash) (models.Transaction, error) {
	val, err := t.store.get(txIDKey, ID.Bytes())
	if err != nil {
		return nil, err
	}

	return models.UnmarshalTransaction(val)
}
