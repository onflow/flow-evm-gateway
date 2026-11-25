package pebble

import (
	"testing"

	pebbleDB "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/storage/mocks"
)

func TestTruncateStorage(t *testing.T) {
	t.Run("truncate removes blocks above target height", func(t *testing.T) {
		db, store, blocks, receipts, traces := setupTruncateTestDB(t)
		defer func() {
			require.NoError(t, db.Close())
		}()

		// Store 10 blocks (heights 1-10)
		storeTestBlocks(t, store, blocks, 1, 10)

		// Verify we have 10 blocks
		latestHeight, err := blocks.LatestEVMHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(10), latestHeight)

		// Truncate to height 5
		truncator := NewTruncateStorage(store, blocks, receipts, traces, zerolog.Nop())
		err = truncator.Truncate(5)
		require.NoError(t, err)

		// Verify latest height is now 5
		latestHeight, err = blocks.LatestEVMHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(5), latestHeight)

		// Verify blocks 1-5 still exist
		for h := uint64(1); h <= 5; h++ {
			_, err := blocks.GetByHeight(h)
			require.NoError(t, err, "block %d should exist", h)
		}

		// Verify blocks 6-10 are gone
		for h := uint64(6); h <= 10; h++ {
			_, err := blocks.GetByHeight(h)
			require.Error(t, err, "block %d should not exist", h)
		}
	})

	t.Run("truncate does nothing when target is at or above latest", func(t *testing.T) {
		db, store, blocks, receipts, traces := setupTruncateTestDB(t)
		defer func() {
			require.NoError(t, db.Close())
		}()

		// Store 5 blocks
		storeTestBlocks(t, store, blocks, 1, 5)

		// Truncate to height 5 (same as latest)
		truncator := NewTruncateStorage(store, blocks, receipts, traces, zerolog.Nop())
		err := truncator.Truncate(5)
		require.NoError(t, err)

		// Verify all blocks still exist
		latestHeight, err := blocks.LatestEVMHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(5), latestHeight)

		// Truncate to height 10 (above latest)
		err = truncator.Truncate(10)
		require.NoError(t, err)

		// Verify all blocks still exist
		latestHeight, err = blocks.LatestEVMHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(5), latestHeight)
	})

	t.Run("truncate processes in batches", func(t *testing.T) {
		db, store, blocks, receipts, traces := setupTruncateTestDB(t)
		defer func() {
			require.NoError(t, db.Close())
		}()

		// Store 250 blocks (more than TruncateBatchSize)
		storeTestBlocks(t, store, blocks, 1, 250)

		// Verify we have 250 blocks
		latestHeight, err := blocks.LatestEVMHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(250), latestHeight)

		// Truncate to height 50
		truncator := NewTruncateStorage(store, blocks, receipts, traces, zerolog.Nop())
		err = truncator.Truncate(50)
		require.NoError(t, err)

		// Verify latest height is now 50
		latestHeight, err = blocks.LatestEVMHeight()
		require.NoError(t, err)
		assert.Equal(t, uint64(50), latestHeight)

		// Verify blocks 1-50 still exist
		for h := uint64(1); h <= 50; h++ {
			_, err := blocks.GetByHeight(h)
			require.NoError(t, err, "block %d should exist", h)
		}
	})
}

func TestGetEVMHeightForCadenceHeight(t *testing.T) {
	t.Run("finds correct EVM height for Cadence height", func(t *testing.T) {
		db, store, blocks, _, _ := setupTruncateTestDB(t)
		defer func() {
			require.NoError(t, db.Close())
		}()

		// Store blocks with different Cadence heights
		// Block 1 at Cadence height 100
		// Block 2 at Cadence height 101
		// Block 3 at Cadence height 101 (same Cadence block)
		// Block 4 at Cadence height 102
		storeBlockAtCadenceHeight(t, store, blocks, 1, 100)
		storeBlockAtCadenceHeight(t, store, blocks, 2, 101)
		storeBlockAtCadenceHeight(t, store, blocks, 3, 101)
		storeBlockAtCadenceHeight(t, store, blocks, 4, 102)

		// Find EVM height for Cadence height 101
		evmHeight, err := blocks.GetEVMHeightForCadenceHeight(101)
		require.NoError(t, err)
		assert.Equal(t, uint64(3), evmHeight, "should return highest EVM height at or below Cadence height 101")

		// Find EVM height for Cadence height 100
		evmHeight, err = blocks.GetEVMHeightForCadenceHeight(100)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), evmHeight)

		// Find EVM height for Cadence height 102
		evmHeight, err = blocks.GetEVMHeightForCadenceHeight(102)
		require.NoError(t, err)
		assert.Equal(t, uint64(4), evmHeight)
	})
}

func TestRegisterTruncateAtHeight(t *testing.T) {
	t.Run("truncate removes registers above target height", func(t *testing.T) {
		dir := t.TempDir()
		db, err := OpenDB(dir)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, db.Close())
		}()

		store := New(db, zerolog.Nop())
		owner := flowGo.Address{0x1}
		registerStore := NewRegisterStorage(store, owner)

		// Store registers at different heights
		for height := uint64(1); height <= 10; height++ {
			entries := []flowGo.RegisterEntry{
				{
					Key: flowGo.RegisterID{
						Owner: string(owner[:]),
						Key:   "test-key",
					},
					Value: []byte{byte(height)},
				},
			}
			batch := store.NewBatch()
			err := registerStore.Store(entries, height, batch)
			require.NoError(t, err)
			err = batch.Commit(pebbleDB.Sync)
			require.NoError(t, err)
		}

		// Truncate at height 5
		err = registerStore.TruncateAtHeight(5)
		require.NoError(t, err)

		// Verify registers at heights 1-5 still exist
		for height := uint64(1); height <= 5; height++ {
			val, err := registerStore.Get(flowGo.RegisterID{
				Owner: string(owner[:]),
				Key:   "test-key",
			}, height)
			require.NoError(t, err)
			assert.NotNil(t, val, "register at height %d should exist", height)
		}

		// Verify registers at heights 6-10 are gone
		// Note: Get returns the value at or below the requested height,
		// so we need to check that querying at height 10 returns height 5's value
		val, err := registerStore.Get(flowGo.RegisterID{
			Owner: string(owner[:]),
			Key:   "test-key",
		}, 10)
		require.NoError(t, err)
		assert.Equal(t, []byte{5}, val, "should return value from height 5")
	})
}

// Helper functions

func setupTruncateTestDB(t *testing.T) (*pebbleDB.DB, *Storage, *Blocks, *Receipts, *Traces) {
	dir := t.TempDir()
	db, err := OpenDB(dir)
	require.NoError(t, err)

	store := New(db, zerolog.Nop())
	blocks := NewBlocks(store, flowGo.Emulator)
	receipts := NewReceipts(store)
	traces := NewTraces(store)

	// Initialize heights
	batch := store.NewBatch()
	err = blocks.InitHeights(0, flow.Identifier{0x1}, batch)
	require.NoError(t, err)
	err = batch.Commit(pebbleDB.Sync)
	require.NoError(t, err)

	return db, store, blocks, receipts, traces
}

func storeTestBlocks(t *testing.T, store *Storage, blocks *Blocks, startHeight, endHeight uint64) {
	var prevBlock = mocks.NewBlock(startHeight)

	batch := store.NewBatch()
	err := blocks.Store(startHeight+100, flow.Identifier{byte(startHeight)}, prevBlock, batch)
	require.NoError(t, err)
	err = batch.Commit(pebbleDB.Sync)
	require.NoError(t, err)

	for h := startHeight + 1; h <= endHeight; h++ {
		block := mocks.NewBlockWithParent(h, prevBlock)

		batch := store.NewBatch()
		err := blocks.Store(h+100, flow.Identifier{byte(h)}, block, batch)
		require.NoError(t, err)
		err = batch.Commit(pebbleDB.Sync)
		require.NoError(t, err)

		prevBlock = block
	}
}

func storeBlockAtCadenceHeight(t *testing.T, store *Storage, blocks *Blocks, evmHeight, cadenceHeight uint64) {
	block := mocks.NewBlock(evmHeight)

	batch := store.NewBatch()
	err := blocks.Store(cadenceHeight, flow.Identifier{byte(evmHeight)}, block, batch)
	require.NoError(t, err)
	err = batch.Commit(pebbleDB.Sync)
	require.NoError(t, err)
}
