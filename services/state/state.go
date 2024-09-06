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
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

type State struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	block         *models.Block
	blocks        storage.BlockIndexer
	receipts      storage.ReceiptIndexer
	logger        zerolog.Logger
}

func NewState(
	block *models.Block,
	ledger atree.Ledger,
	chainID flowGo.ChainID,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) (*State, error) {
	logger = logger.With().Str("component", "state-execution").Logger()
	storageAddress := evm.StorageAccountAddress(chainID)

	// todo do we need state db?
	s, err := state.NewStateDB(ledger, storageAddress)
	if err != nil {
		return nil, err
	}

	emu := emulator.NewEmulator(ledger, storageAddress)

	// todo optimization: we could init the state block view here instead of on each tx execution
	// but we would need to append all precompile calls since they are needed for
	// block context

	return &State{
		StateDB:  s,
		emulator: emu,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
		logger:   logger,
	}, nil
}

func (s *State) Execute(tx models.Transaction) error {
	l := s.logger.With().Str("tx-hash", tx.Hash().String()).Logger()
	l.Info().Msg("executing new transaction")

	receipt, err := s.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return err
	}

	blockCtx, err := s.blockContext(receipt)
	if err != nil {
		return err
	}

	bv, err := s.emulator.NewBlockView(blockCtx)
	if err != nil {
		return err
	}

	var res *types.Result

	switch tx.(type) {
	case models.DirectCall:
		t := tx.(models.DirectCall)
		res, err = bv.DirectCall(t.DirectCall)
	case models.TransactionCall:
		t := tx.(models.TransactionCall)
		res, err = bv.RunTransaction(t.Transaction)
	default:
		return fmt.Errorf("invalid transaction type")
	}

	if err != nil {
		// todo is this ok, the service would restart and retry?
		return err
	}

	// we should never produce invalid transaction, since if the transaction was emitted from the evm core
	// it must have either been successful or failed, invalid transactions are not emitted
	if res.Invalid() {
		return fmt.Errorf("invalid transaction %s: %w", tx.Hash(), res.ValidationError)
	}

	if ok, errs := models.EqualReceipts(res.Receipt(), receipt); !ok {
		return fmt.Errorf("state missmatch: %v", errs)
	}

	l.Debug().Msg("transaction executed successfully")
	return nil
}

func (s *State) blockContext(receipt *models.Receipt) (types.BlockContext, error) {
	calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
	if err != nil {
		return types.BlockContext{}, err
	}

	precompileContracts := precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)

	return types.BlockContext{
		ChainID:                types.FlowEVMPreviewNetChainID, // todo configure dynamically
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
		Random:                    s.block.PrevRandao,
		ExtraPrecompiledContracts: precompileContracts,
		// todo check values bellow if they are needed by the execution
		TxCountSoFar:      0,
		TotalGasUsedSoFar: 0,
		Tracer:            nil,
	}, nil
}
