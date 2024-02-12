package pebble

import (
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBlock(t *testing.T) {

	t.Run("store block", func(t *testing.T) {
		db, err := New()
		require.NoError(t, err)

		block := storage.NewBlock(10)
		db.storeBlock()
	})
}
