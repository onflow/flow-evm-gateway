package evm

import (
	"fmt"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/offchain/storage"
	flowGo "github.com/onflow/flow-go/model/flow"
)

func ExtractEVMState(
	chainID flowGo.ChainID,
	evmHeight uint64,
	store *pebble.Storage,
) (*state.EVMState, error) {
	storageRoot := evm.StorageAccountAddress(chainID)
	registerStore := pebble.NewRegisterStorage(store, storageRoot)
	snapshot, err := registerStore.GetSnapshotAt(evmHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot at evm height %d: %w", evmHeight, err)
	}

	ledger := storage.NewReadOnlyStorage(snapshot)
	bv, err := state.NewBaseView(ledger, storageRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create base view: %w", err)
	}

	evmState, err := state.Extract(storageRoot, bv)
	if err != nil {
		return nil, err
	}
	return evmState, nil
}
