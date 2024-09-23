package evm

import (
	"fmt"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/precompiles"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

type BlockExecutor struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	chainID       flowGo.ChainID
	block         *models.Block
	blocks        storage.BlockIndexer
	logger        zerolog.Logger
	receipts      storage.ReceiptIndexer

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
) (*BlockExecutor, error) {
	logger = logger.With().Str("component", "state-execution").Logger()
	storageAddress := evm.StorageAccountAddress(chainID)

	stateDB, err := state.NewStateDB(ledger, storageAddress)
	if err != nil {
		return nil, err
	}

	return &BlockExecutor{
		emulator: emulator.NewEmulator(ledger, storageAddress),
		StateDB:  stateDB,
		chainID:  chainID,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
		logger:   logger,
	}, nil
}

func (s *BlockExecutor) Run(
	tx models.Transaction,
	tracer *tracers.Tracer,
) (*gethTypes.Receipt, error) {
	l := s.logger.With().Str("tx-hash", tx.Hash().String()).Logger()
	l.Info().Msg("executing new transaction")

	receipt, err := s.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return nil, err
	}

	ctx, err := s.blockContext(receipt)
	ctx.Tracer = tracer
	if err != nil {
		return nil, err
	}

	bv, err := s.emulator.NewBlockView(ctx)
	if err != nil {
		return nil, err
	}

	var res *types.Result

	switch t := tx.(type) {
	case models.DirectCall:
		res, err = bv.DirectCall(t.DirectCall)
	case models.TransactionCall:
		res, err = bv.RunTransaction(t.Transaction)
	default:
		return nil, fmt.Errorf("invalid transaction type")
	}

	if err != nil {
		// todo is this ok, the service would restart and retry?
		return nil, err
	}

	// we should never produce invalid transaction, since if the transaction was emitted from the evm core
	// it must have either been successful or failed, invalid transactions are not emitted
	if res.Invalid() {
		return nil, fmt.Errorf("invalid transaction %s: %w", tx.Hash(), res.ValidationError)
	}

	// increment values as part of a virtual block
	s.gasUsed += res.GasConsumed
	s.txIndex++

	l.Debug().Msg("transaction executed successfully")

	return res.LightReceipt().ToReceipt(), nil
}

func (s *BlockExecutor) Call(
	from common.Address,
	data []byte,
	tracer *tracers.Tracer,
) (*types.Result, error) {
	ctx, err := s.blockContext(nil)
	ctx.Tracer = tracer
	if err != nil {
		return nil, err
	}

	bv, err := s.emulator.NewBlockView(ctx)
	if err != nil {
		return nil, err
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(data); err != nil {
		return nil, err
	}

	return bv.DryRunTransaction(tx, from)
}

// blockContext produces a context that is used by the block view during the execution.
// It can be used for transaction execution and calls. Receipt is not required when
// producing the context for calls.
func (s *BlockExecutor) blockContext(receipt *models.Receipt) (types.BlockContext, error) {
	ctx := types.BlockContext{
		ChainID:                types.EVMChainIDFromFlowChainID(s.chainID),
		BlockNumber:            s.block.Height,
		BlockTimestamp:         s.block.Timestamp,
		DirectCallBaseGasUsage: types.DefaultDirectCallBaseGasUsage, // todo check
		DirectCallGasPrice:     types.DefaultDirectCallGasPrice,
		GasFeeCollector:        types.CoinbaseAddress,
		GetHashFunc: func(n uint64) common.Hash {
			b, err := s.blocks.GetByHeight(n)
			if err != nil {
				panic(err)
			}
			h, err := b.Hash()
			if err != nil {
				panic(err)
			}

			return h
		},
		Random:            s.block.PrevRandao,
		TxCountSoFar:      s.txIndex,
		TotalGasUsedSoFar: s.gasUsed,
		// todo what to do with the tracer
		Tracer: nil,
	}

	// only add precompile cadence arch mocks if we have a receipt,
	// in case of call and dry run we don't produce receipts
	// todo when a call is made that uses cadence arch precompiles, it will fail, because
	// the precompiled contracts won't be set since we don't have a receipt for them
	// this failure should be detected and we should in such a case execute a call against the
	// EN using an AN
	if receipt != nil {
		calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
		if err != nil {
			return types.BlockContext{}, err
		}

		ctx.ExtraPrecompiledContracts = precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)
	}

	return ctx, nil
}
