package replayer

import (
	"fmt"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go/fvm/evm/offchain/blocks"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
)

type blockSnapshot struct {
	*BlocksProvider
	block models.Block
}

var _ evmTypes.BlockSnapshot = (*blockSnapshot)(nil)

func (bs *blockSnapshot) BlockContext() (evmTypes.BlockContext, error) {
	return blocks.NewBlockContext(
		bs.chainID,
		bs.block.Height,
		bs.block.Timestamp,
		func(n uint64) gethCommon.Hash {
			block, err := bs.blocks.GetByHeight(n)
			if err != nil {
				return gethCommon.Hash{}
			}
			blockHash, err := block.Hash()
			if err != nil {
				return gethCommon.Hash{}
			}

			return blockHash
		},
		bs.block.PrevRandao,
		bs.tracer,
	)
}

// This BlocksProvider implementation is used in the EVM events ingestion pipeline.
// The ingestion module notifies the BlocksProvider of incoming EVM blocks, by
// calling the `OnBlockReceived` method. This method guarantees that blocks are
// processed sequentially, and keeps track of the latest block, which is used
// for generating the proper `BlockContext`. This is necessary for replaying
// EVM blocks/transactions locally, and verifying that there are no state
// mismatches.
type BlocksProvider struct {
	blocks      storage.BlockIndexer
	chainID     flowGo.ChainID
	tracer      *tracers.Tracer
	latestBlock *models.Block
}

var _ evmTypes.BlockSnapshotProvider = (*BlocksProvider)(nil)

func NewBlocksProvider(
	blocks storage.BlockIndexer,
	chainID flowGo.ChainID,
	tracer *tracers.Tracer,
) *BlocksProvider {
	return &BlocksProvider{
		blocks:  blocks,
		chainID: chainID,
		tracer:  tracer,
	}
}

func (bp *BlocksProvider) OnBlockReceived(block *models.Block) error {
	if bp.latestBlock != nil && bp.latestBlock.Height != (block.Height-1) {
		return fmt.Errorf(
			"%w: received new block: %d, non-sequential of latest block: %d",
			models.ErrInvalidHeight,
			block.Height,
			bp.latestBlock.Height,
		)
	}

	bp.latestBlock = block

	return nil
}

func (bp *BlocksProvider) GetSnapshotAt(height uint64) (
	evmTypes.BlockSnapshot,
	error,
) {
	if bp.latestBlock != nil && bp.latestBlock.Height == height {
		return &blockSnapshot{
			BlocksProvider: bp,
			block:          *bp.latestBlock,
		}, nil
	}

	block, err := bp.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	return &blockSnapshot{
		BlocksProvider: bp,
		block:          *block,
	}, nil
}
