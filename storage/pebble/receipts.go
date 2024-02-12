package pebble

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	"math/big"
	"sync"
)

var _ storage.ReceiptIndexer = &Receipts{}

type Receipts struct {
	store *Storage
	mux   sync.RWMutex
}

func NewReceipts(store *Storage) *Receipts {
	return &Receipts{
		store: store,
		mux:   sync.RWMutex{},
	}
}

func (r *Receipts) Store(receipt *gethTypes.Receipt) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	val, err := receipt.MarshalBinary()
	if err != nil {
		return err
	}

	// todo batch the operations
	if err := r.store.set(receiptTxIDKey, receipt.TxHash, val); err != nil {
		return err
	}

	if err := r.store.set(receiptHeightKey, receipt.BlockNumber, val); err != nil {
		return err
	}

	return r.store.set(bloomHeightKey, receipt.BlockNumber, receipt.Bloom.Bytes())
}

func (r *Receipts) GetByTransactionID(ID common.Hash) (*gethTypes.Receipt, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	return r.getReceipt(receiptTxIDKey, ID)
}

func (r *Receipts) GetByBlockHeight(height *big.Int) (*gethTypes.Receipt, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	return r.getReceipt(receiptHeightKey, height)
}

func (r *Receipts) BloomsForBlockRange(start, end *big.Int) ([]gethTypes.Bloom, []*big.Int, error) {
	panic("TODO")
}

func (r *Receipts) getReceipt(keyCode byte, key any) (*gethTypes.Receipt, error) {
	val, err := r.store.get(keyCode, key)
	if err != nil {
		return nil, err
	}

	var receipt *gethTypes.Receipt
	return receipt, receipt.UnmarshalBinary(val)
}
