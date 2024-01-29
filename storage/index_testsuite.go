package storage

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage/errors"
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

type ReceiptTestSuite struct {
	suite.Suite
	ReceiptIndexer ReceiptIndexer
}

func (s *ReceiptTestSuite) TestStoreReceipt() {
	receipt := newReceipt(1, common.HexToHash("0xf1"))

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
		receipt := newReceipt(2, common.HexToHash("0xf2"))
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
		ID := common.HexToHash("0x1")
		receipt := newReceipt(3, ID)
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)

		retReceipt, err := s.ReceiptIndexer.GetByBlockID(ID)
		s.Require().NoError(err)
		s.Require().Equal(receipt, retReceipt)
	})

	s.Run("non-existing block ID", func() {
		nonExistingBlockHash := common.HexToHash("0x456")
		retReceipt, err := s.ReceiptIndexer.GetByBlockID(nonExistingBlockHash)
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.NotFound)
	})
}

func (s *ReceiptTestSuite) TestBloomsForBlockRange() {

	s.Run("valid block range", func() {
		start := big.NewInt(10)
		end := big.NewInt(15)
		testBlooms := make([]types.Bloom, 0)

		for i := start; i.Cmp(end) < 0; i = i.Add(i, big.NewInt(1)) {
			r := newReceipt(i.Uint64(), common.HexToHash(fmt.Sprintf("0xf1%d", i)))
			testBlooms = append(testBlooms, r.Bloom)
		}

		blooms, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().NoError(err)
		s.Require().Len(blooms, len(testBlooms))
		s.Require().Equal(testBlooms, blooms)
	})

	s.Run("invalid block range", func() {
		start := big.NewInt(10)
		end := big.NewInt(5) // end is less than start
		blooms, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().ErrorIs(err, errors.InvalidRange)
		s.Require().Nil(blooms)
	})

	s.Run("non-existing block range", func() {
		start := big.NewInt(100)
		end := big.NewInt(105)
		blooms, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().NoError(err)
		s.Require().Len(blooms, 0)
	})
}

type TransactionTestSuite struct {
	suite.Suite
	TransactionIndexer TransactionIndexer
}

func (s *TransactionTestSuite) TestStoreTransaction() {
	tx := newTransaction(0)

	s.Run("store transaction successfully", func() {
		err := s.TransactionIndexer.Store(tx)
		s.Require().NoError(err)
	})

	s.Run("store duplicate transaction", func() {
		err := s.TransactionIndexer.Store(tx)
		s.Require().ErrorIs(err, errors.Duplicate)
	})
}

func (s *TransactionTestSuite) TestGetTransaction() {
	s.Run("existing transaction", func() {
		tx := newTransaction(1)
		err := s.TransactionIndexer.Store(tx)
		s.Require().NoError(err)

		retTx, err := s.TransactionIndexer.Get(tx.Hash())
		s.Require().NoError(err)
		s.Require().Equal(tx, retTx)
	})

	s.Run("non-existing transaction", func() {
		nonExistingTxHash := common.HexToHash("0x789")
		retTx, err := s.TransactionIndexer.Get(nonExistingTxHash)
		s.Require().Nil(retTx)
		s.Require().ErrorIs(err, errors.NotFound)
	})
}
