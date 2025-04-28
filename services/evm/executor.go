package evm

import (
	"fmt"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/offchain/blocks"
	"github.com/onflow/flow-go/fvm/evm/precompiles"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

type BlockExecutor struct {
	emulator types.Emulator
	chainID  flowGo.ChainID
	block    *models.Block
	blocks   storage.BlockIndexer
	logger   zerolog.Logger
	receipts storage.ReceiptIndexer

	// block dynamic data
	txIndex uint
	gasUsed uint64
}

func NewBlockExecutor(
	block *models.Block,
	ledger atree.Ledger,
	chainID flowGo.ChainID,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) *BlockExecutor {
	logger = logger.With().Str("component", "trace-generation").Logger()
	storageAddress := evm.StorageAccountAddress(chainID)

	return &BlockExecutor{
		emulator: emulator.NewEmulator(ledger, storageAddress),
		chainID:  chainID,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
		logger:   logger,
	}
}

func (s *BlockExecutor) Run(
	tx models.Transaction,
	tracer *tracers.Tracer,
) error {
	l := s.logger.With().Str("tx-hash", tx.Hash().String()).Logger()
	l.Info().Msg("executing new transaction")

	receipt, err := s.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return err
	}

	ctx, err := s.blockContext(receipt, tracer)
	if err != nil {
		return err
	}

	bv, err := s.emulator.NewBlockView(ctx)
	if err != nil {
		return err
	}

	var res *types.Result

	switch t := tx.(type) {
	case models.DirectCall:
		res, err = bv.DirectCall(t.DirectCall)
	case models.TransactionCall:
		res, err = bv.RunTransaction(t.Transaction)
	default:
		return fmt.Errorf("invalid transaction type")
	}

	if err != nil {
		return err
	}

	// we should never produce invalid transaction, since if the transaction was emitted from the evm core
	// it must have either been successful or failed, invalid transactions are not emitted
	if res.Invalid() {
		return fmt.Errorf("invalid transaction %s: %w", tx.Hash(), res.ValidationError)
	}

	// increment values as part of a virtual block
	s.gasUsed += res.GasConsumed
	s.txIndex++

	if res.GasConsumed != receipt.GasUsed {
		return fmt.Errorf(
			"used gas mismatch, expected: %d, got: %d",
			receipt.GasUsed,
			res.GasConsumed,
		)
	}
	l.Debug().Msg("transaction executed successfully")

	return nil
}

// blockContext produces a context that is used by the block view during the execution.
// It can be used for transaction execution and calls. Receipt is not required when
// producing the context for calls.
func (s *BlockExecutor) blockContext(
	receipt *models.Receipt,
	tracer *tracers.Tracer,
) (types.BlockContext, error) {
	ctx, err := blocks.NewBlockContext(
		s.chainID,
		s.block.Height,
		s.block.Timestamp,
		func(n uint64) common.Hash {
			block, err := s.blocks.GetByHeight(n)
			if err != nil {
				return common.Hash{}
			}
			blockHash, err := block.Hash()
			if err != nil {
				return common.Hash{}
			}

			return blockHash
		},
		s.block.PrevRandao,
		tracer,
	)

	if err != nil {
		return types.BlockContext{}, err
	}

	ctx.TxCountSoFar = s.txIndex
	ctx.TotalGasUsedSoFar = s.gasUsed

	// only add precompile cadence arch contract if we have a receipt
	if receipt != nil {
		calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
		if err != nil {
			return types.BlockContext{}, err
		}

		ctx.ExtraPrecompiledContracts = precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)
	}

	return ctx, nil
}
