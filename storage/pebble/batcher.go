package pebble

import (
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.Batcher = &Batch{}

type Batch struct {
	*pebble.Batch
}

func NewBatch(db *pebble.DB) *Batch {
	return &Batch{db.NewBatch()}
}

func (b *Batch) Commit() error {
	if err := b.Batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit index storage batch: %w", err)
	}
	return nil
}

func (b *Batch) Close() error {
	return b.Batch.Close()
}
