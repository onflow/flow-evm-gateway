package replayer

import (
	"fmt"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
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
	return evmTypes.BlockContext{
		ChainID:                evmTypes.EVMChainIDFromFlowChainID(bs.chainID),
		BlockNumber:            bs.block.Height,
		BlockTimestamp:         bs.block.Timestamp,
		DirectCallBaseGasUsage: evmTypes.DefaultDirectCallBaseGasUsage,
		DirectCallGasPrice:     evmTypes.DefaultDirectCallGasPrice,
		GasFeeCollector:        evmTypes.CoinbaseAddress,
		GetHashFunc: func(n uint64) gethCommon.Hash {
			// For block heights greater than or equal to the current,
			// return an empty block hash.
			if n >= bs.block.Height {
				return gethCommon.Hash{}
			}
			// If the given block height, is more than 256 blocks
			// in the past, return an empty block hash.
			if bs.block.Height-n > 256 {
				return gethCommon.Hash{}
			}

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
		Random: bs.block.PrevRandao,
		Tracer: bs.tracer,
	}, nil
}

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
			"received new block: %d, non-sequential of latest block: %d",
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
