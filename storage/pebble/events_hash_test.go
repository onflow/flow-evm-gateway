package pebble

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk"
	"github.com/stretchr/testify/require"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

func TestEventsHash_BatchRemoveAboveHeight(t *testing.T) {
	runDB("removes only entries strictly above threshold", t, func(t *testing.T, db *Storage) {
		eh := NewEventsHash(db)

		for _, h := range []uint64{10, 11, 12, 13, 14, 15} {
			require.NoError(t, eh.Store(h, flow.Identifier{byte(h)}))
		}

		batch := db.NewBatch()
		require.NoError(t, eh.BatchRemoveAboveHeight(12, batch))
		require.NoError(t, batch.Commit(pebble.Sync))

		// heights <= 12 are preserved
		for _, h := range []uint64{10, 11, 12} {
			id, err := eh.GetByHeight(h)
			require.NoError(t, err)
			require.Equal(t, flow.Identifier{byte(h)}, id)
		}

		// heights > 12 are removed
		for _, h := range []uint64{13, 14, 15} {
			_, err := eh.GetByHeight(h)
			require.ErrorIs(t, err, errs.ErrEntityNotFound)
		}
	})

	runDB("no-op when nothing above threshold", t, func(t *testing.T, db *Storage) {
		eh := NewEventsHash(db)

		for _, h := range []uint64{10, 11, 12} {
			require.NoError(t, eh.Store(h, flow.Identifier{byte(h)}))
		}

		batch := db.NewBatch()
		require.NoError(t, eh.BatchRemoveAboveHeight(100, batch))
		require.NoError(t, batch.Commit(pebble.Sync))

		for _, h := range []uint64{10, 11, 12} {
			id, err := eh.GetByHeight(h)
			require.NoError(t, err)
			require.Equal(t, flow.Identifier{byte(h)}, id)
		}
	})

	runDB("does not touch adjacent key prefix", t, func(t *testing.T, db *Storage) {
		eh := NewEventsHash(db)

		require.NoError(t, eh.Store(50, flow.Identifier{0xAA}))
		require.NoError(t, eh.SetProcessedSealedHeight(99))

		batch := db.NewBatch()
		require.NoError(t, eh.BatchRemoveAboveHeight(10, batch))
		require.NoError(t, batch.Commit(pebble.Sync))

		// events_hash entry above threshold removed
		_, err := eh.GetByHeight(50)
		require.ErrorIs(t, err, errs.ErrEntityNotFound)

		// sealedEventsHeightKey (adjacent code) is untouched
		got, err := eh.ProcessedSealedHeight()
		require.NoError(t, err)
		require.Equal(t, uint64(99), got)
	})

	runDB("uint64 max threshold is a no-op", t, func(t *testing.T, db *Storage) {
		eh := NewEventsHash(db)
		require.NoError(t, eh.Store(42, flow.Identifier{0x2A}))

		batch := db.NewBatch()
		require.NoError(t, eh.BatchRemoveAboveHeight(^uint64(0), batch))
		require.NoError(t, batch.Commit(pebble.Sync))

		id, err := eh.GetByHeight(42)
		require.NoError(t, err)
		require.Equal(t, flow.Identifier{0x2A}, id)
	})
}
