package pebble

import (
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.FeeParametersIndexer = &FeeParameters{}

type FeeParameters struct {
	store *Storage
	mu    sync.Mutex
}

func NewFeeParameters(store *Storage) *FeeParameters {
	return &FeeParameters{
		store: store,
	}
}

func (f *FeeParameters) Store(feeParameters *models.FeeParameters, batch *pebble.Batch) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	val, err := feeParameters.ToBytes()
	if err != nil {
		return err
	}

	if err := f.store.set(feeParametersKey, nil, val, batch); err != nil {
		return fmt.Errorf("failed to store fee parameters %v: %w", feeParameters, err)
	}

	return nil
}

func (f *FeeParameters) Get() (*models.FeeParameters, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := f.store.get(feeParametersKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get fee parameters: %w", err)
	}

	feeParameters, err := models.NewFeeParametersFromBytes(data)
	if err != nil {
		return nil, err
	}

	return feeParameters, nil
}
