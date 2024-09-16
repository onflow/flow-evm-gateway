package state

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
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

// todo reuse the error from flow-go
// cadenceArchUnexpectedCall is returned by cadence arch
// var cadenceArchUnexpectedCall = fmt.Errorf("unexpected call")

type BlockState struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	chainID       flowGo.ChainID
	block         *models.Block
	txIndex       uint
	gasUsed       uint64
	blocks        storage.BlockIndexer
	receipts      storage.ReceiptIndexer
	logger        zerolog.Logger
}

func NewBlockState(
	block *models.Block,
	registers atree.Ledger,
	chainID flowGo.ChainID,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) (*BlockState, error) {
	logger = logger.With().Str("component", "state-execution").Logger()
	storageAddress := evm.StorageAccountAddress(chainID)

	stateDB, err := state.NewStateDB(registers, storageAddress)
	if err != nil {
		return nil, err
	}

	return &BlockState{
		emulator: emulator.NewEmulator(registers, storageAddress),
		StateDB:  stateDB,
		chainID:  chainID,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
		logger:   logger,
	}, nil
}

func (s *BlockState) Execute(tx models.Transaction) (*gethTypes.Receipt, error) {
	l := s.logger.With().Str("tx-hash", tx.Hash().String()).Logger()
	l.Info().Msg("executing new transaction")

	receipt, err := s.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return nil, err
	}

	ctx, err := s.blockContext(receipt)
	if err != nil {
		return err
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

func (s *BlockState) Call(from common.Address, data []byte) (*types.Result, error) {
	// todo
	// executing a transaction for a call or estimate gas that uses
	// cadence arch calls (https://github.com/onflow/flow-go/blob/master/fvm/evm/precompiles/arch.go)
	// will fail, because there was no receipt made by EN that would contain input and output
	// data we use to mock the calls to cadence arch using the replayer in the block context.
	// This defer handles such a panic gracefully, so we can return the response from remote client instead.
	//defer func() {
	//	if r := recover(); r != nil {
	//		if err, ok := r.(error); ok {
	//			if errors.Is(err, cadenceArchUnexpectedCall) {
	//
	//			}
	//		}
	//	}
	//}()

	ctx, err := s.blockContext(nil)
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
func (s *BlockState) blockContext(receipt *models.Receipt) (types.BlockContext, error) {
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
	if receipt != nil {
		calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
		if err != nil {
			return types.BlockContext{}, err
		}

		ctx.ExtraPrecompiledContracts = precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)
	}

	return ctx, nil
}
