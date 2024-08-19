package state

import (
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"

	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

type State struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	evmHeight     uint64
	blocks        storage.BlockIndexer
}

func NewState(
	evmHeight uint64,
	blocks storage.BlockIndexer,
	ledger *pebble.Ledger,
	chainID flowGo.ChainID,
) (*State, error) {
	storageAddress := evm.StorageAccountAddress(chainID)

	// todo do we need state db?
	s, err := state.NewStateDB(ledger, storageAddress)
	if err != nil {
		return nil, err
	}

	emu := emulator.NewEmulator(ledger, storageAddress)

	return &State{
		StateDB:   s,
		emulator:  emu,
		evmHeight: evmHeight,
		blocks:    blocks,
	}, nil
}

func (s *State) Execute(tx *gethTypes.Transaction) error {
	block, err := s.blocks.GetByHeight(s.evmHeight)
	if err != nil {
		return err
	}

	blockCtx := types.BlockContext{
		ChainID:                nil,
		BlockNumber:            block.Height,
		BlockTimestamp:         block.Timestamp,
		DirectCallBaseGasUsage: types.DefaultDirectCallBaseGasUsage, // todo check
		DirectCallGasPrice:     types.DefaultDirectCallGasPrice,
		TxCountSoFar:           0,               // todo check it might not be needed for execution
		TotalGasUsedSoFar:      0,               // todo check it might not be needed for execution
		GasFeeCollector:        types.Address{}, // todo check
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
		Random:                    common.Hash{}, // todo we need to expose rand value used in block
		Tracer:                    nil,           // todo check, but no need for tracer now
		ExtraPrecompiledContracts: nil,           // todo check, but no need for now
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
