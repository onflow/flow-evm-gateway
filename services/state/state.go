package state

import (
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethTypes "github.com/onflow/go-ethereum/core/types"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

type State struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
}

func NewState(ledger *pebble.Ledger) (*State, error) {
	addr := flowGo.HexToAddress("0x01") // todo fix

	// todo do we need state db?
	s, err := state.NewStateDB(ledger, addr)
	if err != nil {
		return nil, err
	}

	emu := emulator.NewEmulator(ledger, addr)

	return &State{
		StateDB:  s,
		emulator: emu,
	}, nil
}

func (s *State) Execute(tx *gethTypes.Transaction) error {

	bv, err := s.emulator.NewBlockView(types.NewDefaultBlockContext(0))
	if err != nil {
		return err
	}

	_, err = bv.RunTransaction(tx)
	if err != nil {
		return err
	}

	return nil
}
