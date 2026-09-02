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

// BatchRemoveAboveHeight removes all stored events hashes above the given height (exclusive).
//
// Emits a single pebble range tombstone over [eventsHashKey||BE(height+1), sealedEventsHeightKey),
// which is O(1) regardless of how many entries were previously stored. The previous per-height
// loop could stall setup for a long time (and balloon the setup batch in memory) when
// --force-start-height rewound past a large populated range — e.g. a DB previously indexed by
// the sealing verifier.
func (e *EventsHash) BatchRemoveAboveHeight(height uint64, batch *pebbleDB.Batch) error {
	// Guard against uint64 overflow when height is math.MaxUint64.
	if height == math.MaxUint64 {
		return nil
	}
	start := makePrefix(eventsHashKey, uint64Bytes(height+1))
	end := makePrefix(sealedEventsHeightKey) // exclusive upper bound: next key-code prefix
	if err := batch.DeleteRange(start, end, nil); err != nil {
		return fmt.Errorf("failed to remove events hashes above height %d: %w", height, err)
	}
	return nil
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
		return e.BatchSetProcessedSealedHeight(height, batch)
	})
}

func (e *EventsHash) BatchSetProcessedSealedHeight(height uint64, batch *pebbleDB.Batch) error {
	if err := e.store.set(sealedEventsHeightKey, nil, uint64Bytes(height), batch); err != nil {
		return fmt.Errorf("failed to store latest processed sealed height: %d, with: %w", height, err)
	}
	return nil
}
