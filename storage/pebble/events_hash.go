package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"

	pebbleDB "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

type EventsHash struct {
	store *Storage
}

func NewEventsHash(store *Storage) *EventsHash {
	return &EventsHash{
		store: store,
	}
}

func (e *EventsHash) Store(height uint64, hash flow.Identifier) error {
	return WithBatch(e.store, func(batch *pebbleDB.Batch) error {
		if err := e.store.set(eventsHashKey, uint64Bytes(height), hash.Bytes(), batch); err != nil {
			return fmt.Errorf(
				"failed to store events hash for block %d, with: %w",
				height,
				err,
			)
		}
		return nil
	})
}

func (e *EventsHash) GetByHeight(height uint64) (flow.Identifier, error) {
	hash, err := e.store.get(eventsHashKey, uint64Bytes(height))
	if err != nil {
		return flow.Identifier{}, fmt.Errorf("failed to get events hash for block %d, with: %w", height, err)
	}

	return flow.BytesToID(hash), nil
}

func (e *EventsHash) Remove(height uint64) error {
	prefixedKey := makePrefix(eventsHashKey, uint64Bytes(height))
	return e.store.db.Delete(prefixedKey, pebbleDB.Sync)
}

func (e *EventsHash) ProcessedSealedHeight() (uint64, error) {
	val, err := e.store.get(sealedEventsHeightKey)
	if err != nil {
		if errors.Is(err, errs.ErrEntityNotFound) {
			return 0, errs.ErrStorageNotInitialized
		}
		return 0, fmt.Errorf("failed to get latest processed sealed height: %w", err)
	}

	return binary.BigEndian.Uint64(val), nil
}

func (e *EventsHash) SetProcessedSealedHeight(height uint64) error {
	return WithBatch(e.store, func(batch *pebbleDB.Batch) error {
		if err := e.store.set(sealedEventsHeightKey, nil, uint64Bytes(height), batch); err != nil {
			return fmt.Errorf("failed to store latest processed sealed height: %d, with: %w", height, err)
		}
		return nil
	})
}
