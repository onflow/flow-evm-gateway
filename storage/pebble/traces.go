package pebble

import (
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/goccy/go-json"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.TraceIndexer = &Traces{}

type Traces struct {
	store *Storage
	mux   sync.RWMutex
}

func NewTraces(store *Storage) *Traces {
	return &Traces{
		store: store,
		mux:   sync.RWMutex{},
	}
}

func (t *Traces) StoreTransaction(ID common.Hash, trace json.RawMessage, batch *pebble.Batch) error {
	t.mux.Lock()
	defer t.mux.Unlock()

	if err := t.store.set(traceTxIDKey, ID.Bytes(), trace, batch); err != nil {
		return fmt.Errorf("failed to store trace for transaction ID %s: %w", ID.String(), err)
	}

	return nil
}

func (t *Traces) GetTransaction(ID common.Hash) (json.RawMessage, error) {
	t.mux.RLock()
	defer t.mux.RUnlock()

	val, err := t.store.get(traceTxIDKey, ID.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to get trace for transaction ID %s: %w", ID.String(), err)
	}

	return val, nil
}
