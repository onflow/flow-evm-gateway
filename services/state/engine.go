package state

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm/precompiles"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ models.Engine = &Engine{}
var _ models.Subscriber = &Engine{}

type Engine struct {
	chainID        flowGo.ChainID
	logger         zerolog.Logger
	status         *models.EngineStatus
	blockPublisher *models.Publisher
	blocks         storage.BlockIndexer
	transactions   storage.TransactionIndexer
	receipts       storage.ReceiptIndexer
	ledger         atree.Ledger
}

func NewStateEngine(
	chainID flowGo.ChainID,
	ledger atree.Ledger,
	blockPublisher *models.Publisher,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) *Engine {
	log := logger.With().Str("component", "state").Logger()

	return &Engine{
		chainID:        chainID,
		logger:         log,
		status:         models.NewEngineStatus(),
		blockPublisher: blockPublisher,
		blocks:         blocks,
		transactions:   transactions,
		receipts:       receipts,
		ledger:         ledger,
	}
}

// todo rethink whether it would be more robust to rely on blocks in the storage
// instead of receiving events, relying on storage and keeping a separate count of
// transactions executed would allow for independent restart and reexecution
// if we panic with events the missed tx won't get reexecuted since it's relying on
// event ingestion also not indexing that transaction

func (e *Engine) Notify(data any) {
	block, ok := data.(*models.Block)
	if !ok {
		e.logger.Error().Msg("invalid event type sent to state ingestion")
		return
	}

	l := e.logger.With().Uint64("evm-height", block.Height).Logger()
	l.Info().Msg("received new block")

	if err := e.executeBlock(block); err != nil {
		panic(fmt.Errorf("failed to execute block at height %d: %w", block.Height, err))
	}

	l.Info().Msg("successfully executed block")
}

func (e *Engine) Run(ctx context.Context) error {
	e.blockPublisher.Subscribe(e)
	e.status.MarkReady()
	return nil
}

func (e *Engine) Stop() {
	// todo cleanup
	e.status.MarkStopped()
}

func (e *Engine) Done() <-chan struct{} {
	return e.status.IsDone()
}

func (e *Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}

func (e *Engine) Error() <-chan error {
	return nil
}

func (e *Engine) ID() uuid.UUID {
	return uuid.New()
}

// executeBlock will execute all transactions in the provided block.
// If a transaction fails to execute or the result doesn't match expected
// result return an error.
// Transaction executed should match a receipt we have indexed from the network
// produced by execution nodes. This check makes sure we keep a correct state.
func (e *Engine) executeBlock(block *models.Block) error {
	state, err := NewState(block, e.ledger, e.chainID, e.blocks, e.receipts, e.logger)
	if err != nil {
		return err
	}

	// track gas usage in a virtual block
	gasUsed := uint64(0)

	for i, h := range block.TransactionHashes {
		e.logger.Info().Str("hash", h.String()).Msg("transaction execution")

		tx, err := e.transactions.Get(h)
		if err != nil {
			return err
		}

		receipt, err := e.receipts.GetByTransactionID(tx.Hash())
		if err != nil {
			return err
		}

		ctx, err := e.blockContext(block, receipt, uint(i), gasUsed)
		if err != nil {
			return err
		}

		resultReceipt, err := state.Execute(ctx, tx)
		if err != nil {
			return err
		}

		// increment the gas used only after it's executed
		gasUsed += receipt.GasUsed

		if ok, errs := models.EqualReceipts(resultReceipt, receipt); !ok {
			return fmt.Errorf("state missmatch: %v", errs)
		}
	}

	return nil
}

func (e *Engine) blockContext(
	block *models.Block,
	receipt *models.Receipt,
	txIndex uint,
	gasUsed uint64,
) (types.BlockContext, error) {
	calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
	if err != nil {
		return types.BlockContext{}, err
	}

	precompileContracts := precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)

	return types.BlockContext{
		ChainID:                types.EVMChainIDFromFlowChainID(e.chainID),
		BlockNumber:            block.Height,
		BlockTimestamp:         block.Timestamp,
		DirectCallBaseGasUsage: types.DefaultDirectCallBaseGasUsage, // todo check
		DirectCallGasPrice:     types.DefaultDirectCallGasPrice,
		GasFeeCollector:        types.CoinbaseAddress,
		GetHashFunc: func(n uint64) common.Hash {
			b, err := e.blocks.GetByHeight(n)
			if err != nil {
				panic(err)
			}
			h, err := b.Hash()
			if err != nil {
				panic(err)
			}

			return h
		},
		Random:                    block.PrevRandao,
		ExtraPrecompiledContracts: precompileContracts,
		TxCountSoFar:              txIndex,
		TotalGasUsedSoFar:         gasUsed,
		// todo what to do with the tracer
		Tracer: nil,
	}, nil
}
