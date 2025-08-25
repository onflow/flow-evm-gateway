package storage_test

import (
	"testing"

	pebble2 "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/goccy/go-json"
	"github.com/onflow/flow-go-sdk"
	"github.com/stretchr/testify/suite"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
)

// tests that make sure the implementation conform to the interface expected behaviour
func TestBlocks(t *testing.T) {
	runDB("blocks", t, func(t *testing.T, db *pebble.Storage) {
		bl := pebble.NewBlocks(db, flowGo.Emulator)
		batch := db.NewBatch()

		err := bl.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)

		err = batch.Commit(pebble2.Sync)
		require.NoError(t, err)

		suite.Run(t, &BlockTestSuite{
			Blocks: bl,
			DB:     db,
		})
	})
}

func TestReceipts(t *testing.T) {
	runDB("receipts", t, func(t *testing.T, db *pebble.Storage) {
		// prepare the blocks database since they track heights which are used in receipts as well
		bl := pebble.NewBlocks(db, flowGo.Emulator)
		batch := db.NewBatch()

		err := bl.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
		require.NoError(t, err)
		err = bl.Store(30, flow.Identifier{0x1}, mocks.NewBlock(10), batch) // update first and latest height
		require.NoError(t, err)
		err = bl.Store(30, flow.Identifier{0x1}, mocks.NewBlock(300), batch) // update latest
		require.NoError(t, err)

		err = batch.Commit(pebble2.Sync)
		require.NoError(t, err)

		suite.Run(t, &ReceiptTestSuite{
			BlocksIndexer:  bl,
			ReceiptIndexer: pebble.NewReceipts(db),
			DB:             db,
		})
	})
}

func TestTransactions(t *testing.T) {
	runDB("transactions", t, func(t *testing.T, db *pebble.Storage) {
		suite.Run(t, &TransactionTestSuite{
			TransactionIndexer: pebble.NewTransactions(db),
			DB:                 db,
		})
	})
}

func TestTraces(t *testing.T) {
	runDB("traces", t, func(t *testing.T, db *pebble.Storage) {
		suite.Run(t, &TraceTestSuite{
			TraceIndexer: pebble.NewTraces(db),
			DB:           db,
		})
	})
}

type BlockTestSuite struct {
	suite.Suite
	Blocks storage.BlockIndexer
	DB     *pebble.Storage
}

func (b *BlockTestSuite) TestGet() {
	b.Run("existing block", func() {
		height := uint64(1)
		flowID := flow.Identifier{0x01}
		block := mocks.NewBlock(height)
		batch := b.DB.NewBatch()

		err := b.Blocks.Store(height+1, flowID, block, batch)
		b.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
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
		batch := b.DB.NewBatch()

		err := b.Blocks.Store(2, flowID, block, batch)
		b.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		b.Require().NoError(err)

		batch = b.DB.NewBatch()

		// we allow overwriting blocks to make the actions idempotent
		err = b.Blocks.Store(2, flowID, block, batch)
		b.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		b.Require().NoError(err)
	})

	b.Run("store multiple blocks, and get one", func() {

		for i := range 10 {
			batch := b.DB.NewBatch()

			err := b.Blocks.Store(uint64(i+5), flow.Identifier{byte(i)}, mocks.NewBlock(uint64(10+i)), batch)
			b.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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
		for i := range 5 {
			lastHeight := uint64(100 + i)
			batch := b.DB.NewBatch()

			err := b.Blocks.Store(lastHeight+10, flow.Identifier{byte(i)}, mocks.NewBlock(lastHeight), batch)
			b.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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
			batch := b.DB.NewBatch()

			err := b.Blocks.Store(uint64(i), cadenceIDs[i], blocks[i], batch)
			b.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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
		for i := range 5 {
			lastHeight := uint64(100 + i)
			batch := b.DB.NewBatch()
			err := b.Blocks.Store(lastHeight, flow.Identifier{byte(i)}, mocks.NewBlock(lastHeight-10), batch)
			b.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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
			batch := b.DB.NewBatch()
			err := b.Blocks.Store(cadenceHeights[i], flow.Identifier{byte(i)}, mocks.NewBlock(evmHeight), batch)
			b.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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
			batch := b.DB.NewBatch()
			err := b.Blocks.Store(uint64(i), cadenceIDs[i], mocks.NewBlock(evmHeight), batch)
			b.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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
	BlocksIndexer  storage.BlockIndexer
	ReceiptIndexer storage.ReceiptIndexer
	DB             *pebble.Storage
}

func (s *ReceiptTestSuite) TestStoreReceipt() {

	s.Run("store receipt successfully", func() {
		batch := s.DB.NewBatch()
		flowID := flow.Identifier{0x01}
		block := mocks.NewBlock(1)

		err := s.BlocksIndexer.Store(block.Height+1, flowID, block, batch)
		s.Require().NoError(err)

		receipt := mocks.NewReceipt(block)
		err = s.ReceiptIndexer.Store([]*models.Receipt{receipt}, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)
	})

	s.Run("store multiple receipts at same height", func() {
		const height = 5
		batch := s.DB.NewBatch()
		flowID := flow.Identifier{0x01}
		block := mocks.NewBlock(height)

		err := s.BlocksIndexer.Store(height+1, flowID, block, batch)
		s.Require().NoError(err)

		receipts := []*models.Receipt{
			mocks.NewReceipt(block),
			mocks.NewReceipt(block),
			mocks.NewReceipt(block),
		}
		// Log index field holds the index position in the entire block
		logIndex := uint(0)
		for _, receipt := range receipts {
			for _, log := range receipt.Logs {
				log.Index = logIndex
				logIndex++
			}
		}

		err = s.ReceiptIndexer.Store(receipts, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)

		storeReceipts, err := s.ReceiptIndexer.GetByBlockHeight(height)
		s.Require().NoError(err)

		for i, sr := range storeReceipts {
			s.compareReceipts(receipts[i], sr)
		}
	})

	s.Run("fail to store multiple receipts with different heights", func() {
		batch := s.DB.NewBatch()
		flowID := flow.Identifier{0x01}
		block1 := mocks.NewBlock(1)

		err := s.BlocksIndexer.Store(block1.Height+1, flowID, block1, batch)
		s.Require().NoError(err)

		block2 := mocks.NewBlock(2)

		err = s.BlocksIndexer.Store(block2.Height+1, flowID, block2, batch)
		s.Require().NoError(err)

		receipts := []*models.Receipt{
			mocks.NewReceipt(block1),
			mocks.NewReceipt(block2),
		}

		err = s.ReceiptIndexer.Store(receipts, batch)
		s.Require().EqualError(err, "can't store receipts for multiple heights")
	})
}

func (s *ReceiptTestSuite) TestGetReceiptByTransactionID() {
	s.Run("existing transaction ID", func() {
		batch := s.DB.NewBatch()
		flowID := flow.Identifier{0x01}
		block := mocks.NewBlock(2)

		err := s.BlocksIndexer.Store(block.Height+1, flowID, block, batch)
		s.Require().NoError(err)

		receipt := mocks.NewReceipt(block)
		err = s.ReceiptIndexer.Store([]*models.Receipt{receipt}, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
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
		batch := s.DB.NewBatch()
		flowID := flow.Identifier{0x01}
		block := mocks.NewBlock(3)

		err := s.BlocksIndexer.Store(block.Height+1, flowID, block, batch)
		s.Require().NoError(err)

		receipt := mocks.NewReceipt(block)
		err = s.ReceiptIndexer.Store([]*models.Receipt{receipt}, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)

		batch = s.DB.NewBatch()

		// add one more receipt that shouldn't be retrieved
		flowID = flow.Identifier{0x04}
		block4 := mocks.NewBlock(4)

		err = s.BlocksIndexer.Store(block4.Height+1, flowID, block4, batch)
		s.Require().NoError(err)

		r := mocks.NewReceipt(block4)
		s.Require().NoError(s.ReceiptIndexer.Store([]*models.Receipt{r}, batch))

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)

		retReceipts, err := s.ReceiptIndexer.GetByBlockHeight(receipt.BlockNumber.Uint64())
		s.Require().NoError(err)
		s.compareReceipts(receipt, retReceipts[0])
	})

	s.Run("non-existing block height", func() {
		retReceipt, err := s.ReceiptIndexer.GetByBlockHeight(1337)
		s.Require().NoError(err)
		s.Require().Len(retReceipt, 0)
	})
}

func (s *ReceiptTestSuite) TestBloomsForBlockRange() {

	s.Run("valid block range", func() {
		start := uint64(10)
		end := uint64(15)
		testBlooms := make([]*types.Bloom, 0)
		testHeights := make([]uint64, 0)

		for i := start; i < end; i++ {
			batch := s.DB.NewBatch()
			flowID := flow.Identifier{0x0i}
			block := mocks.NewBlock(i)

			err := s.BlocksIndexer.Store(block.Height+1, flowID, block, batch)
			s.Require().NoError(err)

			r := mocks.NewReceipt(block)
			testBlooms = append(testBlooms, &r.Bloom)
			testHeights = append(testHeights, i)
			batch = s.DB.NewBatch()
			err = s.ReceiptIndexer.Store([]*models.Receipt{r}, batch)
			s.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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

		for i := range subsetSize {
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
			batch := s.DB.NewBatch()
			flowID := flow.Identifier{0x0i}
			block := mocks.NewBlock(i)

			err := s.BlocksIndexer.Store(block.Height+1, flowID, block, batch)
			s.Require().NoError(err)

			r1 := mocks.NewReceipt(block)
			r2 := mocks.NewReceipt(block)
			receipts := []*models.Receipt{r1, r2}

			batch = s.DB.NewBatch()
			s.Require().NoError(s.ReceiptIndexer.Store(receipts, batch))
			err = batch.Commit(pebble2.Sync)
			s.Require().NoError(err)

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
			batch := s.DB.NewBatch()
			flowID := flow.Identifier{0x0i}
			block := mocks.NewBlock(i)

			err := s.BlocksIndexer.Store(block.Height+1, flowID, block, batch)
			s.Require().NoError(err)

			r1 := mocks.NewReceipt(block)
			receipts := []*models.Receipt{r1}

			batch = s.DB.NewBatch()
			s.Require().NoError(s.ReceiptIndexer.Store(receipts, batch))

			err = batch.Commit(pebble2.Sync)
			s.Require().NoError(err)

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
	TransactionIndexer storage.TransactionIndexer
	DB                 *pebble.Storage
}

func (s *TransactionTestSuite) TestStoreTransaction() {
	tx := mocks.NewTransaction(0)

	s.Run("store transaction successfully", func() {
		batch := s.DB.NewBatch()

		err := s.TransactionIndexer.Store(tx, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)
	})
}

func (s *TransactionTestSuite) TestGetTransaction() {
	s.Run("existing transaction", func() {
		tx := mocks.NewTransaction(1)
		batch := s.DB.NewBatch()
		err := s.TransactionIndexer.Store(tx, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)

		txHash := tx.Hash()

		retTx, err := s.TransactionIndexer.Get(txHash)
		s.Require().NoError(err)

		retTxHash := retTx.Hash()
		s.Require().Equal(txHash, retTxHash) // if hashes are equal the data must be equal

		batch = s.DB.NewBatch()
		// allow same transaction overwrites
		s.Require().NoError(s.TransactionIndexer.Store(retTx, batch))

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)
	})

	s.Run("store multiple transactions and get single", func() {
		var tx models.Transaction
		for i := range 10 {
			tx = mocks.NewTransaction(uint64(10 + i))
			batch := s.DB.NewBatch()
			err := s.TransactionIndexer.Store(tx, batch)
			s.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
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

type TraceTestSuite struct {
	suite.Suite
	TraceIndexer storage.TraceIndexer
	DB           *pebble.Storage
}

func (s *TraceTestSuite) TestStore() {
	s.Run("store new trace", func() {
		id := common.Hash{0x01}
		trace := json.RawMessage(`{ "test": "foo" }`)

		batch := s.DB.NewBatch()

		err := s.TraceIndexer.StoreTransaction(id, trace, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
		s.Require().NoError(err)
	})

	s.Run("overwrite existing trace", func() {
		for range 2 {
			id := common.Hash{0x01}
			trace := json.RawMessage(`{ "test": "foo" }`)

			batch := s.DB.NewBatch()

			err := s.TraceIndexer.StoreTransaction(id, trace, batch)
			s.Require().NoError(err)

			err = batch.Commit(pebble2.Sync)
			s.Require().NoError(err)
		}
	})
}

func (s *TraceTestSuite) TestGet() {
	s.Run("get existing trace", func() {
		id := common.Hash{0x01}
		trace := json.RawMessage(`{ "test": "foo" }`)

		batch := s.DB.NewBatch()

		err := s.TraceIndexer.StoreTransaction(id, trace, batch)
		s.Require().NoError(err)

		err = batch.Commit(pebble2.Sync)
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
