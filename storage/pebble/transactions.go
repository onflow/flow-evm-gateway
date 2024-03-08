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

func (t *Transactions) Store(evmTxData models.FlowEVMTxData) error {
	t.mux.Lock()
	defer t.mux.Unlock()

	val, err := evmTxData.MarshalBinary()
	if err != nil {
		return err
	}

	evmTxHash, err := evmTxData.Hash()
	if err != nil {
		return err
	}

	return t.store.set(txIDKey, evmTxHash.Bytes(), val, nil)
}

func (t *Transactions) Get(ID common.Hash) (models.FlowEVMTxData, error) {
	t.mux.RLock()
	defer t.mux.RUnlock()

	val, err := t.store.get(txIDKey, ID.Bytes())
	if err != nil {
		return nil, err
	}

	var evmTxData models.FlowEVMTxData

	if val[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(val)
		if err != nil {
			return nil, fmt.Errorf("failed to rlp decode direct call: %w", err)
		}

		evmTxData = models.DirectCallTx{DirectCall: directCall}
	} else {
		tx := &gethTypes.Transaction{}
		if err := tx.UnmarshalBinary(val[1:]); err != nil {
			return nil, fmt.Errorf("failed to rlp decode transaction: %w", err)
		}

		evmTxData = models.GethTx{Transaction: tx}
	}

	return evmTxData, nil
}
