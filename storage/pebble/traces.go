package pebble

import (
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/goccy/go-json"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.TraceIndexer = &Traces{}

type Traces struct {
	store *Storage
}

func NewTraces(store *Storage) *Traces {
	return &Traces{
		store: store,
	}
}

func (t *Traces) StoreTransaction(ID common.Hash, trace json.RawMessage, batch *pebble.Batch) error {
	if err := t.store.set(traceTxIDKey, ID.Bytes(), trace, batch); err != nil {
		return fmt.Errorf("failed to store trace for transaction ID %s: %w", ID.String(), err)
	}

	return nil
}

func (t *Traces) GetTransaction(ID common.Hash) (json.RawMessage, error) {
	val, err := t.store.get(traceTxIDKey, ID.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to get trace for transaction ID %s: %w", ID.String(), err)
	}

	return val, nil
}
