package state

import (
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/precompiles"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

type State struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	block         *models.Block
	blocks        storage.BlockIndexer
	receipts      storage.ReceiptIndexer
}

func NewState(
	evmHeight uint64,
	blocks storage.BlockIndexer,
	ledger *pebble.Ledger,
	chainID flowGo.ChainID,
) (*State, error) {
	storageAddress := evm.StorageAccountAddress(chainID)

	block, err := blocks.GetByHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	// todo do we need state db?
	s, err := state.NewStateDB(ledger, storageAddress)
	if err != nil {
		return nil, err
	}

	emu := emulator.NewEmulator(ledger, storageAddress)

	return &State{
		StateDB:  s,
		emulator: emu,
		block:    block,
		blocks:   blocks,
	}, nil
}

func (s *State) Execute(tx *gethTypes.Transaction) error {
	blockCtx, err := s.blockContext(tx.Hash())
	if err != nil {
		return err
	}

	bv, err := s.emulator.NewBlockView(blockCtx)
	if err != nil {
		return err
	}

	_, err = bv.RunTransaction(tx)
	if err != nil {
		return err
	}

	return nil
}

func (s *State) blockContext(hash common.Hash) (types.BlockContext, error) {
	receipt, err := s.receipts.GetByTransactionID(hash)
	if err != nil {
		return types.BlockContext{}, err
	}

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
		GasFeeCollector:        types.Address(receipt.Coinbase),
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
		Random:                    receipt.Random,
		ExtraPrecompiledContracts: precompileContracts,
		// todo check values bellow if they are needed by the execution
		TxCountSoFar:      0,
		TotalGasUsedSoFar: 0,
		Tracer:            nil,
	}, nil
}
