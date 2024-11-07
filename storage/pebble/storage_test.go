package pebble

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
)

// tests that make sure the implementation conform to the interface expected behaviour
func TestBlocks(t *testing.T) {
	runDB("blocks", t, func(t *testing.T, db *Storage) {
		bl := NewBlocks(db, flowGo.Emulator)
		batch := db.NewBatch()

		err := bl.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)

		err = batch.Commit(pebble.Sync)
		require.NoError(t, err)

		suite.Run(t, &storage.BlockTestSuite{Blocks: bl})
	})
}

func TestReceipts(t *testing.T) {
	runDB("receipts", t, func(t *testing.T, db *Storage) {
		// prepare the blocks database since they track heights which are used in receipts as well
		bl := NewBlocks(db, flowGo.Emulator)
		batch := db.NewBatch()

		err := bl.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)
		err = bl.Store(30, flow.Identifier{0x1}, mocks.NewBlock(10), batch) // update first and latest height
		require.NoError(t, err)
		err = bl.Store(30, flow.Identifier{0x1}, mocks.NewBlock(300), batch) // update latest
		require.NoError(t, err)

		err = batch.Commit(pebble.Sync)
		require.NoError(t, err)

		suite.Run(t, &storage.ReceiptTestSuite{ReceiptIndexer: NewReceipts(db)})
	})
}

func TestTransactions(t *testing.T) {
	runDB("transactions", t, func(t *testing.T, db *Storage) {
		suite.Run(t, &storage.TransactionTestSuite{TransactionIndexer: NewTransactions(db)})
	})
}

func TestAccounts(t *testing.T) {
	runDB("accounts", t, func(t *testing.T, db *Storage) {
		suite.Run(t, &storage.AccountTestSuite{AccountIndexer: NewAccounts(db)})
	})
}

func TestTraces(t *testing.T) {
	runDB("traces", t, func(t *testing.T, db *Storage) {
		suite.Run(t, &storage.TraceTestSuite{TraceIndexer: NewTraces(db)})
	})
}

func TestBlock(t *testing.T) {

	runDB("store block", t, func(t *testing.T, db *Storage) {
		bl := mocks.NewBlock(10)
		blocks := NewBlocks(db, flowGo.Emulator)
		batch := db.NewBatch()

		err := blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)

		err = blocks.Store(20, flow.Identifier{0x1}, bl, batch)
		require.NoError(t, err)

		err = batch.Commit(pebble.Sync)
		require.NoError(t, err)
	})

	runDB("get stored block", t, func(t *testing.T, db *Storage) {
		const height = uint64(12)
		cadenceID := flow.Identifier{0x1}
		cadenceHeight := uint64(20)
		bl := mocks.NewBlock(height)

		blocks := NewBlocks(db, flowGo.Emulator)
		batch := db.NewBatch()
		err := blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)

		err = blocks.Store(cadenceHeight, cadenceID, bl, batch)
		require.NoError(t, err)

		err = batch.Commit(pebble.Sync)
		require.NoError(t, err)

		block, err := blocks.GetByHeight(height)
		require.NoError(t, err)
		assert.Equal(t, bl, block)

		id, err := bl.Hash()
		require.NoError(t, err)

		block, err = blocks.GetByID(id)
		require.NoError(t, err)
		assert.Equal(t, bl, block)

		h, err := blocks.GetCadenceHeight(height)
		require.NoError(t, err)
		require.Equal(t, cadenceHeight, h)

		cid, err := blocks.GetCadenceID(height)
		require.NoError(t, err)
		require.Equal(t, cadenceID, cid)
	})

	runDB("get not found block error", t, func(t *testing.T, db *Storage) {
		blocks := NewBlocks(db, flowGo.Emulator)

		batch := db.NewBatch()
		err := blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)
		err = blocks.Store(2, flow.Identifier{0x1}, mocks.NewBlock(1), batch) // init
		require.NoError(t, err)

		err = batch.Commit(pebble.Sync)
		require.NoError(t, err)

		bl, err := blocks.GetByHeight(11)
		require.ErrorIs(t, err, errors.ErrEntityNotFound)
		require.Nil(t, bl)

		bl, err = blocks.GetByID(common.Hash{0x1})
		require.ErrorIs(t, err, errors.ErrEntityNotFound)
		require.Nil(t, bl)
	})
}

func TestAccount(t *testing.T) {
	t.Run("encoding decoding nonce data", func(t *testing.T) {
		nonce := uint64(10)
		height := uint64(20)
		raw := encodeNonce(10, 20)
		decNonce, decHeight, err := decodeNonce(raw)
		require.NoError(t, err)
		assert.Equal(t, nonce, decNonce)
		assert.Equal(t, height, decHeight)
	})
}

func TestBatch(t *testing.T) {
	runDB("batch successfully stores", t, func(t *testing.T, db *Storage) {
		blocks := NewBlocks(db, flowGo.Emulator)
		trace := NewTraces(db)

		batch := db.NewIndexedBatch()
		defer func() {
			require.NoError(t, batch.Close())
		}()

		height := uint64(5)
		err := blocks.SetLatestCadenceHeight(height, batch)
		require.NoError(t, err)

		raw := json.RawMessage{0x2}
		id := common.Hash{0x3}
		err = trace.StoreTransaction(id, raw, batch)
		require.NoError(t, err)

		require.NoError(t, batch.Commit(pebble.Sync))

		h, err := blocks.LatestCadenceHeight()
		require.NoError(t, err)
		require.Equal(t, height, h)

		tt, err := trace.GetTransaction(id)
		require.NoError(t, err)
		require.Equal(t, raw, tt)
	})

	runDB("should not contain data without committing", t, func(t *testing.T, db *Storage) {
		blocks := NewBlocks(db, flowGo.Emulator)

		batch := db.NewIndexedBatch()
		defer func() {
			require.NoError(t, batch.Close())
		}()

		height := uint64(5)
		err := blocks.SetLatestCadenceHeight(height, batch)
		require.NoError(t, err)

		_, err = blocks.LatestCadenceHeight()
		require.ErrorIs(t, err, errors.ErrStorageNotInitialized)
	})

	runDB("multiple batch stores", t, func(t *testing.T, db *Storage) {
		blocks := NewBlocks(db, flowGo.Emulator)

		for i := 0; i < 5; i++ {
			cadenceHeight := uint64(1 + i)
			evmHeight := uint64(10 + i)
			bl := mocks.NewBlock(evmHeight)

			batch := db.NewIndexedBatch()

			err := blocks.Store(cadenceHeight, flow.HexToID("0x1"), bl, batch)
			require.NoError(t, err)

			err = batch.Commit(pebble.Sync)
			require.NoError(t, err)

			dbBlock, err := blocks.GetByHeight(evmHeight)
			require.NoError(t, err)
			require.Equal(t, bl, dbBlock)

			dbEVM, err := blocks.LatestEVMHeight()
			require.NoError(t, err)
			require.Equal(t, evmHeight, dbEVM)

			dbCadence, err := blocks.LatestCadenceHeight()
			require.NoError(t, err)
			require.Equal(t, cadenceHeight, dbCadence)
		}
	})
}

func runDB(name string, t *testing.T, f func(t *testing.T, db *Storage)) {
	dir := t.TempDir()

	db, err := New(dir, zerolog.New(zerolog.NewTestWriter(t)))
	require.NoError(t, err)

	t.Run(name, func(t *testing.T) {
		f(t, db)
	})
}
