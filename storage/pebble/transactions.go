package pebble

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go/fvm/evm/types"
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

	txHash, err := tx.Hash()
	if err != nil {
		return err
	}

	return t.store.set(txIDKey, txHash.Bytes(), val, nil)
}

func (t *Transactions) Get(ID common.Hash) (models.Transaction, error) {
	t.mux.RLock()
	defer t.mux.RUnlock()

	val, err := t.store.get(txIDKey, ID.Bytes())
	if err != nil {
		return nil, err
	}

	if val[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(val)
		if err != nil {
			return nil, fmt.Errorf("failed to rlp decode direct call: %w", err)
		}

		return models.DirectCall{DirectCall: directCall}, nil
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(val[1:]); err != nil {
		return nil, fmt.Errorf("failed to rlp decode transaction: %w", err)
	}

	return models.TransactionCall{Transaction: tx}, nil
}
