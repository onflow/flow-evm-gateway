package requester

import (
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
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
	blockContext, err := blocks.NewBlockContext(
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
	if err != nil {
		return evmTypes.BlockContext{}, err
	}

	if bs.blockOverrides == nil {
		return blockContext, nil
	}

	if bs.blockOverrides.Number != nil {
		blockContext.BlockNumber = bs.blockOverrides.Number.ToInt().Uint64()
	}

	if bs.blockOverrides.Time != nil {
		blockContext.BlockTimestamp = uint64(*bs.blockOverrides.Time)
	}

	if bs.blockOverrides.Random != nil {
		blockContext.Random = *bs.blockOverrides.Random
	}

	if bs.blockOverrides.Coinbase != nil {
		blockContext.GasFeeCollector = evmTypes.NewAddress(*bs.blockOverrides.Coinbase)
	}

	return blockContext, nil
}

// This BlocksProvider implementation is only used for the `eth_call` &
// `debug_traceCall` JSON-RPC endpoints. It accepts optional `Tracer` &
// `BlockOverrides` objects, which are used when constructing the
// `BlockContext` object.
type BlocksProvider struct {
	blocks         storage.BlockIndexer
	chainID        flowGo.ChainID
	tracer         *tracers.Tracer
	blockOverrides *ethTypes.BlockOverrides
}

var _ evmTypes.BlockSnapshotProvider = (*BlocksProvider)(nil)

func NewBlocksProvider(
	blocks storage.BlockIndexer,
	chainID flowGo.ChainID,
) *BlocksProvider {
	return &BlocksProvider{
		blocks:  blocks,
		chainID: chainID,
	}
}

func (bp *BlocksProvider) WithTracer(tracer *tracers.Tracer) *BlocksProvider {
	return &BlocksProvider{
		blocks:         bp.blocks,
		chainID:        bp.chainID,
		tracer:         tracer,
		blockOverrides: bp.blockOverrides,
	}
}

func (bp *BlocksProvider) WithBlockOverrides(
	blockOverrides *ethTypes.BlockOverrides,
) *BlocksProvider {
	return &BlocksProvider{
		blocks:         bp.blocks,
		chainID:        bp.chainID,
		tracer:         bp.tracer,
		blockOverrides: blockOverrides,
	}
}

func (bp *BlocksProvider) GetSnapshotAt(height uint64) (
	evmTypes.BlockSnapshot,
	error,
) {
	block, err := bp.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	return &blockSnapshot{
		BlocksProvider: bp,
		block:          *block,
	}, nil
}
