package state

import (
	"fmt"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

type State struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	chainID       flowGo.ChainID
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
		chainID:  chainID,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
		logger:   logger,
	}, nil
}

func (s *State) Execute(ctx types.BlockContext, tx models.Transaction) (*gethTypes.Receipt, error) {
	l := s.logger.With().Str("tx-hash", tx.Hash().String()).Logger()
	l.Info().Msg("executing new transaction")

	bv, err := s.emulator.NewBlockView(ctx)
	if err != nil {
		return nil, err
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

	l.Debug().Msg("transaction executed successfully")
	return res.Receipt(), nil
}
