package storage

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
)

type BlockTestSuite struct {
	suite.Suite
	Blocks BlockIndexer
}

func (b *BlockTestSuite) Get(t *testing.T) {
	t.Run("existing block", func(t *testing.T) {
		block := newBlock(1)
		err := b.Blocks.Store(block)
		require.NoError(t, err)

		ID, err := block.Hash()
		require.NoError(t, err)

		retBlock, err := b.Blocks.GetByID(ID)
		require.NoError(t, err)
		require.Equal(t, block.Height, retBlock.Height)
	})

	t.Run("non-existing block", func(t *testing.T) {
		retBlock, err := b.Blocks.GetByID(common.HexToHash("0x10"))
		require.Nil(t, retBlock)
		require.ErrorIs(t, err, NotFound)
	})
}

func (b *BlockTestSuite) Store() {

}
