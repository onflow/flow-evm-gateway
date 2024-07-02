package pebble

import (
	"testing"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
)

// tests that make sure the implementation conform to the interface expected behaviour
func TestBlocks(t *testing.T) {
	runDB("blocks", t, func(t *testing.T, db *Storage) {
		bl := NewBlocks(db)
		err := bl.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1})
		require.NoError(t, err)
		suite.Run(t, &storage.BlockTestSuite{Blocks: bl})
	})
}

func TestReceipts(t *testing.T) {
	runDB("receipts", t, func(t *testing.T, db *Storage) {
		// prepare the blocks database since they track heights which are used in receipts as well
		bl := NewBlocks(db)
		err := bl.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1})
		require.NoError(t, err)
		err = bl.Store(30, flow.Identifier{0x1}, mocks.NewBlock(10), nil) // update first and latest height
		require.NoError(t, err)
		err = bl.Store(30, flow.Identifier{0x1}, mocks.NewBlock(30), nil) // update latest
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
		blocks := NewBlocks(db)
		err := blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1})
		require.NoError(t, err)

		err = blocks.Store(20, flow.Identifier{0x1}, bl, nil)
		require.NoError(t, err)
	})

	runDB("get stored block", t, func(t *testing.T, db *Storage) {
		const height = uint64(12)
		cadenceID := flow.Identifier{0x1}
		cadenceHeight := uint64(20)
		bl := mocks.NewBlock(height)

		blocks := NewBlocks(db)
		err := blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1})
		require.NoError(t, err)

		err = blocks.Store(cadenceHeight, cadenceID, bl, nil)
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
		blocks := NewBlocks(db)
		err := blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1})
		require.NoError(t, err)
		_ = blocks.Store(2, flow.Identifier{0x1}, mocks.NewBlock(1), nil) // init

		bl, err := blocks.GetByHeight(11)
		require.ErrorIs(t, err, errors.ErrNotFound)
		require.Nil(t, bl)

		bl, err = blocks.GetByID(common.Hash{0x1})
		require.ErrorIs(t, err, errors.ErrNotFound)
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

func runDB(name string, t *testing.T, f func(t *testing.T, db *Storage)) {
	dir := t.TempDir()

	db, err := New(dir, zerolog.New(zerolog.NewTestWriter(t)))
	require.NoError(t, err)

	t.Run(name, func(t *testing.T) {
		f(t, db)
	})
}
