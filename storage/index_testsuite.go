package storage

import (
	"fmt"

	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
)

type BlockTestSuite struct {
	suite.Suite
	Blocks BlockIndexer
}

func (b *BlockTestSuite) TestGet() {
	b.Run("existing block", func() {
		height := uint64(1)
		flowID := flow.Identifier{0x01}
		block := mocks.NewBlock(height)
		err := b.Blocks.Store(height+1, flowID, block, nil)
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
		b.Require().ErrorIs(err, errors.ErrEntityNotFound)

		// non-existing height
		bl, err = b.Blocks.GetByHeight(uint64(200))
		b.Require().Nil(bl)
		b.Require().ErrorIs(err, errors.ErrEntityNotFound)
	})
}

func (b *BlockTestSuite) TestStore() {
	block := mocks.NewBlock(10)

	b.Run("success", func() {
		flowID := flow.Identifier{0x01}
		err := b.Blocks.Store(2, flowID, block, nil)
		b.Require().NoError(err)

		// we allow overwriting blocks to make the actions idempotent
		err = b.Blocks.Store(2, flowID, block, nil)
		b.Require().NoError(err)
	})

	b.Run("store multiple blocks, and get one", func() {
		for i := 0; i < 10; i++ {
			err := b.Blocks.Store(uint64(i+5), flow.Identifier{byte(i)}, mocks.NewBlock(uint64(10+i)), nil)
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
			err := b.Blocks.Store(lastHeight+10, flow.Identifier{byte(i)}, mocks.NewBlock(lastHeight), nil)
			b.Require().NoError(err)

			last, err := b.Blocks.LatestEVMHeight()
			b.Require().NoError(err)
			b.Require().Equal(lastHeight, last)

			last, err = b.Blocks.LatestEVMHeight() // second time it should get it from cache
			b.Require().NoError(err)
			b.Require().Equal(lastHeight, last)
		}
	})

	b.Run("get height by ID", func() {
		evmHeights := []uint64{10, 11, 12, 13}
		cadenceIDs := []flow.Identifier{{0x01}, {0x02}, {0x03}, {0x04}}
		blocks := make([]*models.Block, 4)

		for i, evmHeight := range evmHeights {
			blocks[i] = mocks.NewBlock(evmHeight)
			err := b.Blocks.Store(uint64(i), cadenceIDs[i], blocks[i], nil)
			b.Require().NoError(err)
		}

		for i := range evmHeights {
			id, err := blocks[i].Hash()
			b.Require().NoError(err)
			evm, err := b.Blocks.GetHeightByID(id)
			b.Require().NoError(err)
			b.Assert().Equal(evmHeights[i], evm)
		}
	})

	b.Run("last Cadence height", func() {
		for i := 0; i < 5; i++ {
			lastHeight := uint64(100 + i)
			err := b.Blocks.Store(lastHeight, flow.Identifier{byte(i)}, mocks.NewBlock(lastHeight-10), nil)
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
			err := b.Blocks.Store(cadenceHeights[i], flow.Identifier{byte(i)}, mocks.NewBlock(evmHeight), nil)
			b.Require().NoError(err)
		}

		for i, evmHeight := range evmHeights {
			cadence, err := b.Blocks.GetCadenceHeight(evmHeight)
			b.Require().NoError(err)
			b.Assert().Equal(cadenceHeights[i], cadence)
		}
	})

	b.Run("Cadence ID from EVM height", func() {
		evmHeights := []uint64{10, 11, 12, 13}
		cadenceIDs := []flow.Identifier{{0x01}, {0x02}, {0x03}, {0x04}}
		for i, evmHeight := range evmHeights {
			err := b.Blocks.Store(uint64(i), cadenceIDs[i], mocks.NewBlock(evmHeight), nil)
			b.Require().NoError(err)
		}

		for i, evmHeight := range evmHeights {
			cadence, err := b.Blocks.GetCadenceID(evmHeight)
			b.Require().NoError(err)
			b.Assert().Equal(cadenceIDs[i], cadence)
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
		err := s.ReceiptIndexer.Store([]*models.Receipt{receipt}, nil)
		s.Require().NoError(err)
	})

	s.Run("store multiple receipts at same height", func() {
		const height = 5
		receipts := []*models.Receipt{
			mocks.NewReceipt(height, common.HexToHash("0x1")),
			mocks.NewReceipt(height, common.HexToHash("0x2")),
			mocks.NewReceipt(height, common.HexToHash("0x3")),
		}
		// Log index field holds the index position in the entire block
		logIndex := uint(0)
		for _, receipt := range receipts {
			for _, log := range receipt.Logs {
				log.Index = logIndex
				logIndex++
			}
		}

		err := s.ReceiptIndexer.Store(receipts, nil)
		s.Require().NoError(err)

		storeReceipts, err := s.ReceiptIndexer.GetByBlockHeight(height)
		s.Require().NoError(err)

		for i, sr := range storeReceipts {
			s.compareReceipts(receipts[i], sr)
		}
	})

	s.Run("fail to store multiple receipts with different heights", func() {
		receipts := []*models.Receipt{
			mocks.NewReceipt(1, common.HexToHash("0x1")),
			mocks.NewReceipt(2, common.HexToHash("0x2")),
		}

		err := s.ReceiptIndexer.Store(receipts, nil)
		s.Require().EqualError(err, "can't store receipts for multiple heights")
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByTransactionID() {
	s.Run("existing transaction ID", func() {
		receipt := mocks.NewReceipt(2, common.HexToHash("0xf2"))
		err := s.ReceiptIndexer.Store([]*models.Receipt{receipt}, nil)
		s.Require().NoError(err)

		retReceipt, err := s.ReceiptIndexer.GetByTransactionID(receipt.TxHash)
		s.Require().NoError(err)
		s.compareReceipts(receipt, retReceipt)
	})

	s.Run("non-existing transaction ID", func() {
		nonExistingTxHash := common.HexToHash("0x123")
		retReceipt, err := s.ReceiptIndexer.GetByTransactionID(nonExistingTxHash)
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.ErrEntityNotFound)
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByBlockHeight() {
	s.Run("existing block height", func() {
		receipt := mocks.NewReceipt(3, common.HexToHash("0x1"))
		err := s.ReceiptIndexer.Store([]*models.Receipt{receipt}, nil)
		s.Require().NoError(err)
		// add one more receipt that shouldn't be retrieved
		r := mocks.NewReceipt(4, common.HexToHash("0x2"))
		s.Require().NoError(s.ReceiptIndexer.Store([]*models.Receipt{r}, nil))

		retReceipts, err := s.ReceiptIndexer.GetByBlockHeight(receipt.BlockNumber.Uint64())
		s.Require().NoError(err)
		s.compareReceipts(receipt, retReceipts[0])
	})

	s.Run("non-existing block height", func() {
		retReceipt, err := s.ReceiptIndexer.GetByBlockHeight(1337)
		s.Require().Nil(retReceipt)
		s.Require().ErrorIs(err, errors.ErrEntityNotFound)
	})
}

func (s *ReceiptTestSuite) TestBloomsForBlockRange() {

	s.Run("valid block range", func() {
		start := uint64(10)
		end := uint64(15)
		testBlooms := make([]*types.Bloom, 0)
		testHeights := make([]uint64, 0)

		for i := start; i < end; i++ {
			r := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0xf1%d", i)))
			testBlooms = append(testBlooms, &r.Bloom)
			testHeights = append(testHeights, i)
			err := s.ReceiptIndexer.Store([]*models.Receipt{r}, nil)
			s.Require().NoError(err)
		}

		bloomsHeights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().NoError(err)
		s.Require().Len(bloomsHeights, len(testBlooms))

		for i, bloomHeight := range bloomsHeights {
			s.Require().Len(bloomHeight.Blooms, 1)
			s.Require().Equal(bloomHeight.Blooms[0], testBlooms[i])
			s.Require().Equal(bloomHeight.Height, testHeights[i])
		}

		subset := uint64(13)
		subsetSize := int(subset - start + 1) // +1 because it's inclusive

		bloomsHeights, err = s.ReceiptIndexer.BloomsForBlockRange(start, subset)
		s.Require().NoError(err)
		s.Require().Len(bloomsHeights, subsetSize)

		for i := 0; i < subsetSize; i++ {
			s.Require().Len(bloomsHeights[i].Blooms, 1)
			s.Require().Equal(bloomsHeights[i].Blooms[0], testBlooms[i])
			s.Require().Equal(bloomsHeights[i].Height, testHeights[i])
		}
	})

	s.Run("valid block range with multiple receipts per block", func() {
		start := uint64(15)
		end := uint64(20)
		testBlooms := make([]*types.Bloom, 0)
		testHeights := make([]uint64, 0)

		for i := start; i < end; i++ {
			r1 := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0x%d", i)))
			r2 := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0x%d", i)))
			receipts := []*models.Receipt{r1, r2}

			s.Require().NoError(s.ReceiptIndexer.Store(receipts, nil))

			testBlooms = append(testBlooms, &r1.Bloom, &r2.Bloom)
			testHeights = append(testHeights, i)
		}

		bloomsHeights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().NoError(err)
		s.Require().Equal(len(bloomsHeights), int(end-start))

		bloomIndex := 0
		for i, bh := range bloomsHeights {
			s.Require().Equal(bh.Height, testHeights[i])
			s.Require().Len(bh.Blooms, 2)

			for _, bloom := range bh.Blooms {
				s.Require().Equal(bloom, testBlooms[bloomIndex])
				bloomIndex++
			}
		}

		subset := uint64(17)
		subsetSize := int(subset-start) + 1 // +1 because it's inclusive interval
		bloomsHeights, err = s.ReceiptIndexer.BloomsForBlockRange(start, subset)
		s.Require().NoError(err)
		s.Require().Len(bloomsHeights, subsetSize)

		bloomIndex = 0
		for i, bh := range bloomsHeights {
			s.Require().Equal(bh.Height, testHeights[i])
			s.Require().Len(bh.Blooms, 2)

			for _, bloom := range bh.Blooms {
				s.Require().Equal(bloom, testBlooms[bloomIndex])
				bloomIndex++
			}
		}
	})

	s.Run("single height range", func() {
		start := uint64(256)
		end := uint64(270)
		specific := uint64(260)

		var expectedBloom *types.Bloom
		for i := start; i < end; i++ {
			r1 := mocks.NewReceipt(i, common.HexToHash(fmt.Sprintf("0x%d", i)))
			receipts := []*models.Receipt{r1}
			s.Require().NoError(s.ReceiptIndexer.Store(receipts, nil))

			if i == specific {
				expectedBloom = &r1.Bloom
			}
		}

		bloomsHeights, err := s.ReceiptIndexer.BloomsForBlockRange(specific, specific)
		s.Require().NoError(err)
		s.Require().Len(bloomsHeights, 1)
		s.Require().Len(bloomsHeights[0].Blooms, 1)
		s.Require().Equal(expectedBloom, bloomsHeights[0].Blooms[0])
	})

	s.Run("invalid block range", func() {
		start := uint64(10)
		end := uint64(5) // end is less than start
		bloomsHeights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().ErrorIs(err, errors.ErrInvalidBlockRange)
		s.Require().ErrorContains(
			err,
			"invalid block height range: start value 10 is bigger than end value 5",
		)
		s.Require().Nil(bloomsHeights)
	})

	s.Run("non-existing start height", func() {
		start := uint64(400)
		end := uint64(405)
		bloomsHeights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().ErrorIs(err, errors.ErrInvalidBlockRange)
		s.Require().ErrorContains(
			err,
			"invalid block height range: start value 400 is not within the indexed range of [0 - 300]",
		)
		s.Require().Nil(bloomsHeights)
	})

	s.Run("non-existing end height", func() {
		start := uint64(10)
		end := uint64(405)
		bloomsHeights, err := s.ReceiptIndexer.BloomsForBlockRange(start, end)
		s.Require().ErrorIs(err, errors.ErrInvalidBlockRange)
		s.Require().ErrorContains(
			err,
			"invalid block height range: end value 405 is not within the indexed range of [0 - 300]",
		)
		s.Require().Nil(bloomsHeights)
	})
}

func (s *ReceiptTestSuite) compareReceipts(expected *models.Receipt, actual *models.Receipt) {
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
		err := s.TransactionIndexer.Store(tx, nil)
		s.Require().NoError(err)
	})
}

func (s *TransactionTestSuite) TestGetTransaction() {
	s.Run("existing transaction", func() {
		tx := mocks.NewTransaction(1)
		err := s.TransactionIndexer.Store(tx, nil)
		s.Require().NoError(err)

		txHash := tx.Hash()

		retTx, err := s.TransactionIndexer.Get(txHash)
		s.Require().NoError(err)

		retTxHash := retTx.Hash()
		s.Require().Equal(txHash, retTxHash) // if hashes are equal the data must be equal

		// allow same transaction overwrites
		s.Require().NoError(s.TransactionIndexer.Store(retTx, nil))
	})

	s.Run("store multiple transactions and get single", func() {
		var tx models.Transaction
		for i := 0; i < 10; i++ {
			tx = mocks.NewTransaction(uint64(10 + i))
			err := s.TransactionIndexer.Store(tx, nil)
			s.Require().NoError(err)
		}

		txHash := tx.Hash()

		t, err := s.TransactionIndexer.Get(txHash)
		s.Require().NoError(err)

		tHash := t.Hash()
		s.Require().Equal(txHash, tHash)
	})

	s.Run("non-existing transaction", func() {
		nonExistingTxHash := common.HexToHash("0x789")
		retTx, err := s.TransactionIndexer.Get(nonExistingTxHash)
		s.Require().Nil(retTx)
		s.Require().ErrorIs(err, errors.ErrEntityNotFound)
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

		nonce, err := a.AccountIndexer.GetNonce(from)
		a.Require().NoError(err)
		a.Require().Equal(uint64(0), nonce)

		for i := 1; i < 5; i++ {
			tx := mocks.NewTransaction(0)

			txCall, ok := tx.(models.TransactionCall)
			a.Require().True(ok)

			txHash := tx.Hash()

			rcp := mocks.NewReceipt(uint64(i+5), txHash)
			gethTx, err := types.SignTx(txCall.Transaction, evmEmulator.GetDefaultSigner(), key)
			a.Require().NoError(err)

			tx = models.TransactionCall{Transaction: gethTx}

			err = a.AccountIndexer.Update(tx, rcp, nil)
			a.Require().NoError(err)

			nonce, err = a.AccountIndexer.GetNonce(from)
			a.Require().NoError(err)
			a.Require().Equal(uint64(i), nonce)
		}

		// if run second time we should still see same nonce values, since they won't be incremented
		// because we track nonce with evm height, and if same height is used twice we don't update
		for i := 1; i < 5; i++ {
			tx := mocks.NewTransaction(0)

			txCall, ok := tx.(models.TransactionCall)
			a.Require().True(ok)

			txHash := tx.Hash()

			rcp := mocks.NewReceipt(uint64(i+5), txHash)
			gethTx, err := types.SignTx(txCall.Transaction, evmEmulator.GetDefaultSigner(), key)
			a.Require().NoError(err)

			tx = models.TransactionCall{Transaction: gethTx}

			err = a.AccountIndexer.Update(tx, rcp, nil)
			a.Require().NoError(err)

			nonce, err = a.AccountIndexer.GetNonce(from)
			a.Require().NoError(err)
			a.Require().Equal(uint64(4), nonce) // always equal to latest nonce
		}
	})
}

type TraceTestSuite struct {
	suite.Suite
	TraceIndexer TraceIndexer
}

func (s *TraceTestSuite) TestStore() {
	s.Run("store new trace", func() {
		id := common.Hash{0x01}
		trace := json.RawMessage(`{ "test": "foo" }`)
		err := s.TraceIndexer.StoreTransaction(id, trace, nil)
		s.Require().NoError(err)
	})

	s.Run("overwrite existing trace", func() {
		for i := 0; i < 2; i++ {
			id := common.Hash{0x01}
			trace := json.RawMessage(`{ "test": "foo" }`)
			err := s.TraceIndexer.StoreTransaction(id, trace, nil)
			s.Require().NoError(err)
		}
	})
}

func (s *TraceTestSuite) TestGet() {
	s.Run("get existing trace", func() {
		id := common.Hash{0x01}
		trace := json.RawMessage(`{ "test": "foo" }`)

		err := s.TraceIndexer.StoreTransaction(id, trace, nil)
		s.Require().NoError(err)

		val, err := s.TraceIndexer.GetTransaction(id)
		s.Require().NoError(err)
		s.Require().Equal(trace, val)
	})

	s.Run("get not found trace", func() {
		id := common.Hash{0x02}
		val, err := s.TraceIndexer.GetTransaction(id)
		s.Require().ErrorIs(err, errors.ErrEntityNotFound)
		s.Require().Nil(val)
	})
}
