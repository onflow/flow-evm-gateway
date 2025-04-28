package replayer

import (
	"testing"

	pebble2 "github.com/cockroachdb/pebble"

	"github.com/goccy/go-json"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// this import is needed for side-effects, because the
	// tracers.DefaultDirectory is relying on the init function
	_ "github.com/onflow/go-ethereum/eth/tracers/native"
)

func TestOnBlockReceived(t *testing.T) {

	t.Run("without latest block", func(t *testing.T) {
		_, blocks := setupBlocksDB(t)

		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, nil)

		block := mocks.NewBlock(1)
		err := blocksProvider.OnBlockReceived(block)
		require.NoError(t, err)
	})

	t.Run("with new block non-sequential to latest block", func(t *testing.T) {
		_, blocks := setupBlocksDB(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, nil)

		block1 := mocks.NewBlock(1)
		err := blocksProvider.OnBlockReceived(block1)
		require.NoError(t, err)

		block2 := mocks.NewBlock(3)
		err = blocksProvider.OnBlockReceived(block2)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"received new block: 3, non-sequential of latest block: 1",
		)
	})

	t.Run("with new block non-sequential to latest block", func(t *testing.T) {
		_, blocks := setupBlocksDB(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, nil)

		block1 := mocks.NewBlock(10)
		err := blocksProvider.OnBlockReceived(block1)
		require.NoError(t, err)

		block2 := mocks.NewBlock(11)
		err = blocksProvider.OnBlockReceived(block2)
		require.NoError(t, err)
	})
}

func TestBlockContext(t *testing.T) {

	t.Run("for latest block", func(t *testing.T) {
		_, blocks := setupBlocksDB(t)
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		block := mocks.NewBlock(1)
		err := blocksProvider.OnBlockReceived(block)
		require.NoError(t, err)

		blockSnapshopt, err := blocksProvider.GetSnapshotAt(block.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshopt.BlockContext()
		require.NoError(t, err)

		assert.Equal(t, evmTypes.FlowEVMPreviewNetChainID, blockContext.ChainID)
		assert.Equal(t, block.Height, blockContext.BlockNumber)
		assert.Equal(t, block.Timestamp, blockContext.BlockTimestamp)
		assert.Equal(t, evmTypes.DefaultDirectCallBaseGasUsage, blockContext.DirectCallBaseGasUsage)
		assert.Equal(t, evmTypes.DefaultDirectCallGasPrice, blockContext.DirectCallGasPrice)
		assert.Equal(t, evmTypes.CoinbaseAddress, blockContext.GasFeeCollector)
		blockHash := blockContext.GetHashFunc(block.Height)
		assert.Equal(t, common.Hash{}, blockHash)
		assert.Equal(t, block.PrevRandao, blockContext.Random)
		assert.Equal(t, tracer, blockContext.Tracer)
	})
}

func TestGetHashFunc(t *testing.T) {
	db, blocks := setupBlocksDB(t)
	missingHeight := uint64(100)

	blockMapping := make(map[uint64]*models.Block, 0)
	for i := uint64(1); i <= 300; i++ {
		// simulate a missing block
		if i == missingHeight {
			continue
		}

		block := mocks.NewBlock(i)
		batch := db.NewBatch()
		err := blocks.Store(i, flow.Identifier{0x1}, block, batch)
		require.NoError(t, err)

		err = batch.Commit(pebble2.Sync)
		require.NoError(t, err)

		blockMapping[i] = block
	}

	t.Run("with requested height >= latest block height", func(t *testing.T) {
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		latestBlock := blockMapping[200]
		err := blocksProvider.OnBlockReceived(latestBlock)
		require.NoError(t, err)

		blockSnapshopt, err := blocksProvider.GetSnapshotAt(latestBlock.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshopt.BlockContext()
		require.NoError(t, err)
		require.Equal(t, latestBlock.Height, blockContext.BlockNumber)

		// GetHashFunc should return empty block hash for block heights >= latest
		blockHash := blockContext.GetHashFunc(latestBlock.Height)
		assert.Equal(t, common.Hash{}, blockHash)

		blockHash = blockContext.GetHashFunc(latestBlock.Height + 1)
		assert.Equal(t, common.Hash{}, blockHash)
	})

	t.Run("with requested height within 256 block height range", func(t *testing.T) {
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		latestBlock := blockMapping[257]
		err := blocksProvider.OnBlockReceived(latestBlock)
		require.NoError(t, err)

		blockSnapshopt, err := blocksProvider.GetSnapshotAt(latestBlock.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshopt.BlockContext()
		require.NoError(t, err)
		require.Equal(t, latestBlock.Height, blockContext.BlockNumber)

		blockHash := blockContext.GetHashFunc(latestBlock.Height - 256)
		expectedBlock := blockMapping[latestBlock.Height-256]
		expectedHash, err := expectedBlock.Hash()
		require.NoError(t, err)
		assert.Equal(t, expectedHash, blockHash)
	})

	t.Run("with requested height outside the 256 block height range", func(t *testing.T) {
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		latestBlock := blockMapping[260]
		err := blocksProvider.OnBlockReceived(latestBlock)
		require.NoError(t, err)

		blockSnapshopt, err := blocksProvider.GetSnapshotAt(latestBlock.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshopt.BlockContext()
		require.NoError(t, err)
		require.Equal(t, latestBlock.Height, blockContext.BlockNumber)

		blockHash := blockContext.GetHashFunc(latestBlock.Height - 259)
		assert.Equal(t, common.Hash{}, blockHash)
	})

	t.Run("with requested height missing from Blocks DB", func(t *testing.T) {
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		latestBlock := blockMapping[260]
		err := blocksProvider.OnBlockReceived(latestBlock)
		require.NoError(t, err)

		blockSnapshopt, err := blocksProvider.GetSnapshotAt(latestBlock.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshopt.BlockContext()
		require.NoError(t, err)
		require.Equal(t, latestBlock.Height, blockContext.BlockNumber)

		blockHash := blockContext.GetHashFunc(missingHeight)
		assert.Equal(t, common.Hash{}, blockHash)
	})
}

func TestGetSnapshotAt(t *testing.T) {

	t.Run("for latest block", func(t *testing.T) {
		_, blocks := setupBlocksDB(t)
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		block := mocks.NewBlock(1)
		err := blocksProvider.OnBlockReceived(block)
		require.NoError(t, err)

		blockSnapshot, err := blocksProvider.GetSnapshotAt(block.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshot.BlockContext()
		require.NoError(t, err)
		assert.Equal(t, block.Height, blockContext.BlockNumber)
		assert.Equal(t, block.Timestamp, blockContext.BlockTimestamp)
		assert.Equal(t, block.PrevRandao, blockContext.Random)
		assert.Equal(t, tracer, blockContext.Tracer)
	})

	t.Run("for historic block", func(t *testing.T) {
		db, blocks := setupBlocksDB(t)
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		block1 := mocks.NewBlock(1)
		batch := db.NewBatch()
		err := blocks.Store(1, flow.Identifier{0x1}, block1, batch)
		require.NoError(t, err)

		err = batch.Commit(pebble2.Sync)
		require.NoError(t, err)

		block2 := mocks.NewBlock(2)
		err = blocksProvider.OnBlockReceived(block2)
		require.NoError(t, err)

		blockSnapshot, err := blocksProvider.GetSnapshotAt(block1.Height)
		require.NoError(t, err)

		blockContext, err := blockSnapshot.BlockContext()
		require.NoError(t, err)
		assert.Equal(t, block1.Height, blockContext.BlockNumber)
		assert.Equal(t, block1.Timestamp, blockContext.BlockTimestamp)
		assert.Equal(t, block1.PrevRandao, blockContext.Random)
		assert.Equal(t, tracer, blockContext.Tracer)
	})

	t.Run("for missing historic block", func(t *testing.T) {
		_, blocks := setupBlocksDB(t)
		tracer := newCallTracer(t)
		blocksProvider := NewBlocksProvider(blocks, flowGo.Emulator, tracer)

		// `block1` is not stored on Blocks DB
		block1 := mocks.NewBlock(1)

		block2 := mocks.NewBlock(2)
		err := blocksProvider.OnBlockReceived(block2)
		require.NoError(t, err)

		_, err = blocksProvider.GetSnapshotAt(block1.Height)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"entity not found",
		)
	})
}

func setupBlocksDB(t *testing.T) (*pebble.Storage, storage.BlockIndexer) {
	dir := t.TempDir()
	pebbleDB, err := pebble.OpenDB(dir)
	require.NoError(t, err)
	db := pebble.New(pebbleDB, zerolog.Nop())
	batch := db.NewBatch()

	chainID := flowGo.Emulator
	blocks := pebble.NewBlocks(db, chainID)

	err = blocks.InitHeights(config.EmulatorInitCadenceHeight, flow.Identifier{0x1}, batch)
	require.NoError(t, err)

	err = batch.Commit(pebble2.Sync)
	require.NoError(t, err)

	return db, blocks
}

func newCallTracer(t *testing.T) *tracers.Tracer {
	tracer, err := tracers.DefaultDirectory.New(
		"callTracer",
		&tracers.Context{},
		json.RawMessage(`{ "onlyTopCall": true }`),
		emulator.DefaultChainConfig,
	)
	require.NoError(t, err)

	return tracer
}
