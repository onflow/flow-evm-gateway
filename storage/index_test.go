package storage

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-evm-gateway/storage/memory"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
)

type blockStorageSuite struct {
	suite.Suite
	blocks BlockIndexer
}

func (b *blockStorageSuite) Get(t *testing.T) {
	t.Run("existing block", func(t *testing.T) {
		block := newBlock(1)
		err := b.blocks.Store(block)
		require.NoError(t, err)

		ID, err := block.Hash()
		require.NoError(t, err)

		retBlock, err := b.blocks.GetByID(ID)
		require.NoError(t, err)
		require.Equal(t, block.Height, retBlock.Height)
	})

	t.Run("non-existing block", func(t *testing.T) {
		retBlock, err := b.blocks.GetByID(common.HexToHash("0x10"))
		require.Nil(t, retBlock)
		require.ErrorIs(t, err, NotFound)
	})
}

func TestStorageSuite(t *testing.T) {
	suite.Run(t, &blockStorageSuite{blocks: memory.NewBlockStorage()})
}

func TestBlockStorage(t *testing.T) {

	t.Run("GetByID", func(t *testing.T) {
		t.Run("Found", func(t *testing.T) {

		})

		t.Run("Not Found", func(t *testing.T) {

		})
	})

	t.Run("Get By Height", func(t *testing.T) {

	})

}
