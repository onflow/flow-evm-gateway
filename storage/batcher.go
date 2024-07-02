package storage

import (
	"fmt"

	"github.com/cockroachdb/pebble"
)

// Batch is a wrapper around pebbleDB batch operations, it is used
// to make multiple indexer operations atomic.
//
// Note: this is used directly by indexer interface which fixes the
// implementation details to pebbleDB which is not ideal, but can be
// optimized at later time if multiple storage system is needed.
type Batch struct {
	*pebble.Batch
}

// NewBatch create a new PebbleDB batch.
func NewBatch(db *pebble.DB) *Batch {
	return &Batch{db.NewBatch()}
}

func (b *Batch) Commit() error {
	if err := b.Batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit index storage batch: %w", err)
	}
	return nil
}

func (b *Batch) Get() *pebble.Batch {
	return b.Batch
}

func (b *Batch) Close() error {
	return b.Batch.Close()
}
