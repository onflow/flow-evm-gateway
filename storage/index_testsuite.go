package storage

import (
	"fmt"
	"math/big"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"
)

type BlockTestSuite struct {
	suite.Suite
	Blocks BlockIndexer
}

func (b *BlockTestSuite) TestGet() {
	b.Run("existing block", func() {
		height := uint64(1)
		block := mocks.NewBlock(height)
		err := b.Blocks.Store(height+1, block)
		b.Require().NoError(err)

		ID, err := block.Hash()
		b.Require().NoError(err)

		retBlock, err := b.Blocks.GetByID(ID)
		b.Require().NoError(err)
		b.Require().Equal(block, retBlock)

		retBlock, err = b.Blocks.GetByHeight(height)
		b.Require().NoError(err)
		b.Require().Equal(block, retBlock)
	})

	b.Run("non-existing block", func() {
		// non-existing id
		bl, err := b.Blocks.GetByID(common.HexToHash("0x10"))
		b.Require().Nil(bl)
		b.Require().ErrorIs(err, errors.ErrNotFound)

		// non-existing height
		bl, err = b.Blocks.GetByHeight(uint64(200))
		b.Require().Nil(bl)
		b.Require().ErrorIs(err, errors.ErrNotFound)
	})
}

func (b *BlockTestSuite) TestStore() {
	block := mocks.NewBlock(10)

	b.Run("success", func() {
		err := b.Blocks.Store(2, block)
		b.Require().NoError(err)

		// we allow overwriting blocks to make the actions idempotent
		err = b.Blocks.Store(2, block)
		b.Require().NoError(err)
	})

	b.Run("store multiple blocks, and get one", func() {
		for i := 0; i < 10; i++ {
			err := b.Blocks.Store(uint64(i+5), mocks.NewBlock(uint64(10+i)))
			b.Require().NoError(err)
		}

		bl, err := b.Blocks.GetByHeight(15)
		b.Require().NoError(err)

		id, err := bl.Hash()
		b.Require().NoError(err)
		blId, err := b.Blocks.GetByID(id)
		b.Require().NoError(err)
		b.Require().Equal(bl, blId)
	})
}

func (b *BlockTestSuite) TestHeights() {

	b.Run("last EVM height", func() {
		for i := 0; i < 5; i++ {
			lastHeight := uint64(100 + i)
			err := b.Blocks.Store(lastHeight+10, mocks.NewBlock(lastHeight))
			b.Require().NoError(err)

			last, err := b.Blocks.LatestEVMHeight()
			b.Require().NoError(err)
			b.Require().Equal(lastHeight, last)
		}
	})

	b.Run("last Cadence height", func() {
		for i := 0; i < 5; i++ {
			lastHeight := uint64(100 + i)
			err := b.Blocks.Store(lastHeight, mocks.NewBlock(lastHeight-10))
			b.Require().NoError(err)

			last, err := b.Blocks.LatestCadenceHeight()
			b.Require().NoError(err)
			b.Require().Equal(lastHeight, last)
		}
	})

	b.Run("Cadence height from EVM height", func() {
		evmHeights := []uint64{10, 11, 12, 13}
		cadenceHeights := []uint64{20, 24, 26, 27}
		for i, evmHeight := range evmHeights {
			err := b.Blocks.Store(cadenceHeights[i], mocks.NewBlock(evmHeight))
			b.Require().NoError(err)
		}

		for i, evmHeight := range evmHeights {
			cadence, err := b.Blocks.GetCadenceHeight(evmHeight)
			b.Require().NoError(err)
			b.Assert().Equal(cadenceHeights[i], cadence)
		}
	})
}

type ReceiptTestSuite struct {
	suite.Suite
	ReceiptIndexer ReceiptIndexer
}

func (s *ReceiptTestSuite) TestStoreReceipt() {

	s.Run("store receipt successfully", func() {
		receipt := mocks.NewReceipt(1, common.HexToHash("0xf1"))
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)
	})

	s.Run("store multiple receipts at same height", func() {
		const height = 5
		receipts := []*types.Receipt{
			mocks.NewReceipt(height, common.HexToHash("0x1")),
			mocks.NewReceipt(height, common.HexToHash("0x2")),
			mocks.NewReceipt(height, common.HexToHash("0x3")),
		}

		for _, r := range receipts {
			err := s.ReceiptIndexer.Store(r)
			s.Require().NoError(err)
		}

		storeReceipts, err := s.ReceiptIndexer.GetByBlockHeight(big.NewInt(height))
		s.Require().NoError(err)

		for i, sr := range storeReceipts {
			s.compareReceipts(receipts[i], sr)
		}
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByTransactionID() {
	s.Run("existing transaction ID", func() {
		receipt := mocks.NewReceipt(2, common.HexToHash("0xf2"))
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)

		retReceipt, err := s.ReceiptIndexer.GetByTransactionID(receipt.TxHash)
		s.Require().NoError(err)
		s.compareReceipts(receipt, retReceipt)
	})

	s.Run("non-existing transaction ID", func() {
		nonExistingTxHash := common.HexToHash("0x123")
		retReceipt, err := s.ReceiptIndexer.GetByTransactionID(nonExistingTxHash)
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.ErrNotFound)
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByBlockHeight() {
	s.Run("existing block height", func() {
		receipt := mocks.NewReceipt(3, common.HexToHash("0x1"))
		err := s.ReceiptIndexer.Store(receipt)
		s.Require().NoError(err)
		// add one more receipt that shouldn't be retrieved
		s.Require().NoError(s.ReceiptIndexer.Store(mocks.NewReceipt(4, common.HexToHash("0x2"))))

		retReceipts, err := s.ReceiptIndexer.GetByBlockHeight(receipt.BlockNumber)
		s.Require().NoError(err)
		s.compareReceipts(receipt, retReceipts[0])
	})

	s.Run("non-existing block height", func() {
		retReceipt, err := s.ReceiptIndexer.GetByBlockHeight(big.NewInt(1337))
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.ErrNotFound)
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

		blooms, heights, err = s.ReceiptIndexer.BloomsForBlockRange(start, big.NewInt(13))
		s.Require().NoError(err)
		s.Require().Len(blooms, 4)
		s.Require().Len(heights, 4)
		s.Require().Equal(testBlooms[0:4], blooms)
	})

	s.Run("valid block range with multiple receipts per block", func() {
		start := big.NewInt(10)
		end := big.NewInt(15)
		testBlooms := make([]*types.Bloom, 0)

		for i := start.Uint64(); i < end.Uint64(); i++ {
			r1 := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0x%d", i)))
			r2 := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0x%d", i)))
			testBlooms = append(testBlooms, &r1.Bloom, &r2.Bloom)
			s.Require().NoError(s.ReceiptIndexer.Store(r1))
			s.Require().NoError(s.ReceiptIndexer.Store(r2))
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
		s.Require().ErrorIs(err, errors.ErrInvalidRange)
		s.Require().Nil(heights)
		s.Require().Nil(blooms)
	})

	s.Run("non-existing block range", func() {
		start := big.NewInt(100)
		end := big.NewInt(105)
		blooms, heights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().ErrorIs(err, errors.ErrInvalidRange)
		s.Require().Nil(blooms)
		s.Require().Nil(heights)
	})
}

func (s *ReceiptTestSuite) compareReceipts(expected *types.Receipt, actual *types.Receipt) {
	s.Require().Equal(expected.BlockNumber, actual.BlockNumber)
	s.Require().Equal(expected.TxHash, actual.TxHash)
	s.Require().Equal(expected.Type, actual.Type)
	s.Require().Equal(expected.PostState, actual.PostState)
	s.Require().Equal(expected.Status, actual.Status)
	s.Require().Equal(expected.CumulativeGasUsed, actual.CumulativeGasUsed)
	s.Require().Equal(expected.Bloom, actual.Bloom)
	s.Require().Equal(len(expected.Logs), len(actual.Logs))
	for i := range expected.Logs {
		s.Require().Equal(expected.Logs[i], actual.Logs[i])
	}
	s.Require().Equal(expected.TxHash, actual.TxHash)
	s.Require().Equal(expected.ContractAddress, actual.ContractAddress)
	s.Require().Equal(expected.GasUsed, actual.GasUsed)
	s.Require().Equal(expected.EffectiveGasPrice, actual.EffectiveGasPrice)
	s.Require().Equal(expected.BlobGasUsed, actual.BlobGasUsed)
	s.Require().Equal(expected.BlockHash, actual.BlockHash)
	s.Require().Equal(expected.BlockNumber, actual.BlockNumber)
	s.Require().Equal(expected.TransactionIndex, actual.TransactionIndex)
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

		txHash, err := tx.Hash()
		s.Require().NoError(err)
		retTx, err := s.TransactionIndexer.Get(txHash)
		s.Require().NoError(err)
		retTxHash, err := retTx.Hash()
		s.Require().NoError(err)
		s.Require().Equal(txHash, retTxHash) // if hashes are equal the data must be equal

		// allow same transaction overwrites
		s.Require().NoError(s.TransactionIndexer.Store(retTx))
	})

	s.Run("store multiple transactions and get single", func() {
		var tx models.Transaction
		for i := 0; i < 10; i++ {
			tx = mocks.NewTransaction(uint64(10 + i))
			err := s.TransactionIndexer.Store(tx)
			s.Require().NoError(err)
		}

		txHash, err := tx.Hash()
		s.Require().NoError(err)
		t, err := s.TransactionIndexer.Get(txHash)
		s.Require().NoError(err)
		tHash, err := t.Hash()
		s.Require().NoError(err)
		s.Require().Equal(txHash, tHash)
		s.Require().NoError(err)
	})

	s.Run("non-existing transaction", func() {
		nonExistingTxHash := common.HexToHash("0x789")
		retTx, err := s.TransactionIndexer.Get(nonExistingTxHash)
		s.Require().Nil(retTx)
		s.Require().ErrorIs(err, errors.ErrNotFound)
	})
}

type AccountTestSuite struct {
	suite.Suite
	AccountIndexer AccountIndexer
}

func (a *AccountTestSuite) TestNonce() {

	a.Run("update account and increase nonce", func() {
		// todo add multiple accounts test
		from := common.HexToAddress("FACF71692421039876a5BB4F10EF7A439D8ef61E")
		rawKey := "f6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"
		key, err := crypto.HexToECDSA(rawKey)
		a.Require().NoError(err)

		nonce, err := a.AccountIndexer.GetNonce(&from)
		a.Require().NoError(err)
		a.Require().Equal(uint64(0), nonce)

		for i := 1; i < 5; i++ {
			tx := mocks.NewTransaction(0)

			txCall, ok := tx.(models.TransactionCall)
			a.Require().True(ok)

			txHash, err := tx.Hash()
			a.Require().NoError(err)

			rcp := mocks.NewReceipt(uint64(i+5), txHash)
			gethTx, err := types.SignTx(txCall.Transaction, evmEmulator.GetDefaultSigner(), key)
			a.Require().NoError(err)

			tx = models.TransactionCall{Transaction: gethTx}

			err = a.AccountIndexer.Update(tx, rcp)
			a.Require().NoError(err)

			nonce, err = a.AccountIndexer.GetNonce(&from)
			a.Require().NoError(err)
			a.Require().Equal(uint64(i), nonce)
		}

		// if run second time we should still see same nonce values, since they won't be incremented
		// because we track nonce with evm height, and if same height is used twice we don't update
		for i := 1; i < 5; i++ {
			tx := mocks.NewTransaction(0)

			txCall, ok := tx.(models.TransactionCall)
			a.Require().True(ok)

			txHash, err := tx.Hash()
			a.Require().NoError(err)

			rcp := mocks.NewReceipt(uint64(i+5), txHash)
			gethTx, err := types.SignTx(txCall.Transaction, evmEmulator.GetDefaultSigner(), key)
			a.Require().NoError(err)

			tx = models.TransactionCall{Transaction: gethTx}

			err = a.AccountIndexer.Update(tx, rcp)
			a.Require().NoError(err)

			nonce, err = a.AccountIndexer.GetNonce(&from)
			a.Require().NoError(err)
			a.Require().Equal(uint64(4), nonce) // always equal to latest nonce
		}
	})
}
