package evm_test

import (
	"fmt"
	"testing"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	evmState "github.com/onflow/flow-evm-gateway/services/evm"
)

func StateDiff(t *testing.T) {
	state1 := extractEVMState(t, flowGo.Testnet, "/var/flow52/evm/data/db", uint64(17724990))
	state2 := evmStateFromCheckpointExtract(t, "/var/flow52/evm-state-from-checkpoint-228901661")

	differences := state.Diff(state1, state2)

	for i, diff := range differences {
		fmt.Printf("Difference %d: %v\n", i, diff)
	}

	require.Len(t, differences, 0)
}

func extractEVMState(
	t *testing.T, chainID flowGo.ChainID,
	registerStoreDir string, evmHeight uint64) *state.EVMState {

	pebbleDB, err := pebble.OpenDB(registerStoreDir)
	require.NoError(t, err)
	store := pebble.New(pebbleDB, log.Logger)

	evmState, err := evmState.ExtractEVMState(chainID, evmHeight, store)
	require.NoError(t, err)
	return evmState
}

func evmStateFromCheckpointExtract(t *testing.T, dir string) *state.EVMState {
	enState, err := state.ImportEVMStateFromGob(dir)
	require.NoError(t, err)
	return enState
}
