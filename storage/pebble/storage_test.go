package pebble

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"os"
	"testing"
)

// tests that make sure the implementation conform to the interface expected behaviour
func TestBlocks(t *testing.T) {
	runDB("blocks", t, func(t *testing.T, db *Storage) {
		bl, err := NewBlocks(db, WithInitHeight(1))
		require.NoError(t, err)
		suite.Run(t, &storage.BlockTestSuite{Blocks: bl})
	})
}

func TestReceipts(t *testing.T) {
	runDB("receipts", t, func(t *testing.T, db *Storage) {
		// prepare the blocks database since they track heights which are used in receipts as well
		bl, err := NewBlocks(db, WithInitHeight(1))
		require.NoError(t, err)
		err = bl.Store(mocks.NewBlock(20)) // update latest height
		require.NoError(t, err)

		suite.Run(t, &storage.ReceiptTestSuite{ReceiptIndexer: NewReceipts(db)})
	})
}

func TestTransactions(t *testing.T) {
	runDB("transactions", t, func(t *testing.T, db *Storage) {
		suite.Run(t, &storage.TransactionTestSuite{TransactionIndexer: NewTransactions(db)})
	})
}

func TestBlock(t *testing.T) {

	runDB("store block", t, func(t *testing.T, db *Storage) {
		bl := mocks.NewBlock(10)
		blocks, err := NewBlocks(db, WithInitHeight(1))
		require.NoError(t, err)

		err = blocks.Store(bl)
		require.NoError(t, err)
	})

	runDB("get stored block", t, func(t *testing.T, db *Storage) {
		const height = uint64(12)
		bl := mocks.NewBlock(height)

		blocks, err := NewBlocks(db, WithInitHeight(1))
		require.NoError(t, err)

		err = blocks.Store(bl)
		require.NoError(t, err)

		block, err := blocks.GetByHeight(height)
		require.NoError(t, err)
		assert.Equal(t, bl, block)

		id, err := bl.Hash()
		require.NoError(t, err)

		block, err = blocks.GetByID(id)
		require.NoError(t, err)
		assert.Equal(t, bl, block)
	})

	runDB("get not found block error", t, func(t *testing.T, db *Storage) {
		blocks, err := NewBlocks(db, WithInitHeight(1))
		require.NoError(t, err)

		bl, err := blocks.GetByHeight(11)
		require.ErrorIs(t, err, errors.ErrNotFound)
		require.Nil(t, bl)

		bl, err = blocks.GetByID(common.Hash{0x1})
		require.ErrorIs(t, err, errors.ErrNotFound)
		require.Nil(t, bl)
	})
}

func runDB(name string, t *testing.T, f func(t *testing.T, db *Storage)) {
	dir, err := os.MkdirTemp("", "flow-testing-temp-")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(dir))
	}()

	db, err := New(dir, zerolog.New(zerolog.NewTestWriter(t)))
	require.NoError(t, err)

	t.Run(name, func(t *testing.T) {
		f(t, db)
	})
}
