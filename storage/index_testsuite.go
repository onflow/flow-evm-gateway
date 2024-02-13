package storage

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/stretchr/testify/suite"
	"math/big"
)

type BlockTestSuite struct {
	suite.Suite
	Blocks BlockIndexer
}

func (b *BlockTestSuite) TestGet() {
	b.Run("existing block", func() {
		height := uint64(1)
		block := mocks.NewBlock(height)
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
	block := mocks.NewBlock(10)

	b.Run("success", func() {
		err := b.Blocks.Store(block)
		b.Require().NoError(err)

		// we allow overwriting blocks to make the actions idempotent
		err = b.Blocks.Store(block)
		b.Require().NoError(err)
	})

	b.Run("store multiple blocks, and get one", func() {
		for i := 0; i < 10; i++ {
			err := b.Blocks.Store(mocks.NewBlock(uint64(10 + i)))
			b.Require().NoError(err)
		}

		bl, err := b.Blocks.GetByHeight(15)
		b.Require().NoError(err)

		id, err := bl.Hash()
		b.Require().NoError(err)
		blId, err := b.Blocks.GetByID(id)
		b.Require().Equal(bl, blId)
	})
}

func (b *BlockTestSuite) TestHeights() {
	b.Run("first height", func() {
		for i := 0; i < 5; i++ {
			first, err := b.Blocks.FirstHeight()
			b.Require().NoError(err)
			b.Require().Equal(uint64(1), first)

			// shouldn't affect first height
			lastHeight := uint64(100 + i)
			err = b.Blocks.Store(mocks.NewBlock(lastHeight))
			b.Require().NoError(err)
		}
	})

	b.Run("last height", func() {
		for i := 0; i < 5; i++ {
			lastHeight := uint64(100 + i)
			err := b.Blocks.Store(mocks.NewBlock(lastHeight))
			b.Require().NoError(err)

			last, err := b.Blocks.LatestHeight()
			b.Require().NoError(err)
			b.Require().Equal(lastHeight, last)
		}
	})
}

type ReceiptTestSuite struct {
	suite.Suite
	ReceiptIndexer ReceiptIndexer
}

func (s *ReceiptTestSuite) TestStoreReceipt() {
	receipt := mocks.NewReceipt(1, common.HexToHash("0xf1"))

	s.Run("store receipt successfully", func() {
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)
	})

	s.Run("store duplicate receipt", func() {
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().ErrorIs(err, errors.Duplicate)
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByTransactionID() {
	s.Run("existing transaction ID", func() {
		receipt := mocks.NewReceipt(2, common.HexToHash("0xf2"))
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)

		retReceipt, err := s.ReceiptIndexer.GetByTransactionID(receipt.TxHash)
		s.Require().NoError(err)
		s.Require().Equal(receipt, retReceipt)
	})

	s.Run("non-existing transaction ID", func() {
		nonExistingTxHash := common.HexToHash("0x123")
		retReceipt, err := s.ReceiptIndexer.GetByTransactionID(nonExistingTxHash)
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.NotFound)
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByBlockID() {
	s.Run("existing block ID", func() {
		receipt := mocks.NewReceipt(3, common.HexToHash("0x1"))
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)

		retReceipt, err := s.ReceiptIndexer.GetByBlockHeight(receipt.BlockNumber)
		s.Require().NoError(err)
		s.Require().Equal(receipt, retReceipt)
	})

	s.Run("non-existing block ID", func() {
		retReceipt, err := s.ReceiptIndexer.GetByBlockHeight(big.NewInt(1337))
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.NotFound)
	})
}

func (s *ReceiptTestSuite) TestBloomsForBlockRange() {

	s.Run("valid block range", func() {
		start := big.NewInt(10)
		end := big.NewInt(15)
		testBlooms := make([]*types.Bloom, 0)

		for i := start.Uint64(); i < end.Uint64(); i++ {
			r := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0xf1%d", i)))
			testBlooms = append(testBlooms, &r.Bloom)
			err := s.ReceiptIndexer.Store(r)
			s.Require().NoError(err)
		}

		blooms, heights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().NoError(err)
		s.Require().Len(blooms, len(testBlooms))
		s.Require().Len(heights, len(testBlooms))
		s.Require().Equal(testBlooms, blooms)
	})

	s.Run("invalid block range", func() {
		start := big.NewInt(10)
		end := big.NewInt(5) // end is less than start
		blooms, heights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().ErrorIs(err, errors.InvalidRange)
		s.Require().Nil(heights)
		s.Require().Nil(blooms)
	})

	s.Run("non-existing block range", func() {
		start := big.NewInt(100)
		end := big.NewInt(105)
		blooms, heights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().NoError(err)
		s.Require().Nil(blooms)
		s.Require().Nil(heights)
	})
}

type TransactionTestSuite struct {
	suite.Suite
	TransactionIndexer TransactionIndexer
}

func (s *TransactionTestSuite) TestStoreTransaction() {
	tx := mocks.NewTransaction(0)

	s.Run("store transaction successfully", func() {
		err := s.TransactionIndexer.Store(tx)
		s.Require().NoError(err)
	})
}

func (s *TransactionTestSuite) TestGetTransaction() {
	s.Run("existing transaction", func() {
		tx := mocks.NewTransaction(1)
		err := s.TransactionIndexer.Store(tx)
		s.Require().NoError(err)

		retTx, err := s.TransactionIndexer.Get(tx.Hash())
		s.Require().NoError(err)
		s.Require().Equal(tx.Hash(), retTx.Hash()) // if hashes are equal the data must be equal

		// allow same transaction overwrites
		s.Require().NoError(s.TransactionIndexer.Store(retTx))
	})

	s.Run("store multiple transactions and get single", func() {
		var tx *types.Transaction
		for i := 0; i < 10; i++ {
			tx = mocks.NewTransaction(uint64(10 + i))
			err := s.TransactionIndexer.Store(tx)
			s.Require().NoError(err)
		}

		t, err := s.TransactionIndexer.Get(tx.Hash())
		s.Require().Equal(tx.Hash(), t.Hash())
		s.Require().NoError(err)
	})

	s.Run("non-existing transaction", func() {
		nonExistingTxHash := common.HexToHash("0x789")
		retTx, err := s.TransactionIndexer.Get(nonExistingTxHash)
		s.Require().Nil(retTx)
		s.Require().ErrorIs(err, errors.NotFound)
	})
}
