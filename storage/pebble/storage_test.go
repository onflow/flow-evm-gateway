package pebble

import (
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestBlock(t *testing.T) {

	runDB("store block", t, func(t *testing.T, db *Storage) {
		bl := mocks.NewBlock(10)

		err := db.storeBlock(bl)
		require.NoError(t, err)
	})
}

func runDB(name string, t *testing.T, f func(t *testing.T, db *Storage)) {
	dir, err := os.MkdirTemp("", "flow-testing-temp-")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(dir))
	}()

	db, err := New(dir)
	require.NoError(t, err)

	t.Run(name, func(t *testing.T) {
		f(t, db)
	})
}
