package storage

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/stretchr/testify/suite"
)

type BlockTestSuite struct {
	suite.Suite
	Blocks BlockIndexer
}

func (b *BlockTestSuite) TestGet() {
	b.Run("existing block", func() {
		height := uint64(1)
		block := newBlock(height)
		err := b.Blocks.Store(block)
		b.Require().NoError(err)

		ID, err := block.Hash()
		b.Require().NoError(err)

		retBlock, err := b.Blocks.GetByID(ID)
		b.Require().NoError(err)
		b.Require().Equal(block, retBlock)

		retBlock, err = b.Blocks.GetByHeight(height)
		b.Require().Equal(block, retBlock)
	})

	b.Run("non-existing block", func() {
		retBlock, err := b.Blocks.GetByID(common.HexToHash("0x10"))
		b.Require().Nil(retBlock)
		b.Require().ErrorIs(err, errors.NotFound)
	})
}

func (b *BlockTestSuite) TestStore() {
	block := newBlock(10)

	b.Run("success", func() {
		err := b.Blocks.Store(block)
		b.Require().NoError(err)
	})

	b.Run("failed to store same block", func() {
		err := b.Blocks.Store(block)
		b.Require().ErrorIs(err, errors.Duplicate)
	})
}

func (b *BlockTestSuite) TestHeights() {
	b.Run("first height", func() {
		first, err := b.Blocks.FirstHeight()
		b.Require().NoError(err)
		b.Require().Equal(uint64(1), first)
	})

	b.Run("last height", func() {
		lastHeight := uint64(100)
		err := b.Blocks.Store(newBlock(lastHeight))
		b.Require().NoError(err)

		last, err := b.Blocks.LatestHeight()
		b.Require().NoError(err)
		b.Require().Equal(lastHeight, last)
	})
}
