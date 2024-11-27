package main_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go-sdk"

	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/offchain/storage"
	"github.com/onflow/flow-go/fvm/evm/testutils"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func TestStateDiff(t *testing.T) {

	state1 := ExtractEVMState(t, flow.Testnet,
		"/var/flow/gw/data/db", uint64(218215348))
	state2 := state.EVMStateFromReplayGobDir(t,
		"/var/flow2/evm-state-from-gobs-218215348/", uint64(218215348))
	// state2 := state.EVMStateFromCheckpointExtract(t, "/var/flow2/evm-state-from-checkpoint-218215348/")

	differences := state.Diff(state1, state2)

	for i, diff := range differences {
		fmt.Printf("Difference %d: %v\n", i, diff)
	}

	require.Len(t, differences, 0)
}

func ExtractEVMState(
	t *testing.T, chainID flowGo.ChainID,
	registerStoreDir string, flowHeight uint64) *state.EVMState {

	store, err := pebble.New(registerStoreDir, log.Logger)
	require.NoError(t, err)

	storageRoot := evm.StorageAccountAddress(chainID)
	registerStore := pebble.NewRegisterStorage(store, storageRoot)
	snapshot, err := registerStore.GetSnapshotAt(flowHeight)
	require.NoError(t, err)

	ledger := storage.NewReadOnlyStorage(snapshot)
	bv, err := state.NewBaseView(ledger, storageRoot)
	require.NoError(t, err)

	evmState, err := state.Extract(storageRoot, bv)
	require.NoError(t, err)
	return evmState
}

func EVMStateFromCheckpointExtract(t *testing.T, dir string) *state.EVMState {
	enState, err := state.ImportEVMStateFromGob("/var/flow2/evm-state-from-gobs-218215348/")
	require.NoError(t, err)
	return enState
}

func EVMStateFromReplayGobDir(t *testing.T, gobDir string, flowHeight uint64) *state.EVMState {
	valueFileName, allocatorFileName := evmStateGobFileNamesByEndHeight(gobDir, flowHeight)
	chainID := flow.Testnet

	allocatorGobs, err := testutils.DeserializeAllocator(allocatorFileName)
	require.NoError(t, err)

	storageRoot := evm.StorageAccountAddress(chainID)
	valuesGob, err := testutils.DeserializeState(valueFileName)
	require.NoError(t, err)

	store := testutils.GetSimpleValueStorePopulated(valuesGob, allocatorGobs)

	bv, err := state.NewBaseView(store, storageRoot)
	require.NoError(t, err)

	evmState, err := state.Extract(storageRoot, bv)
	require.NoError(t, err)
	return evmState
}

func evmStateGobFileNamesByEndHeight(evmStateGobDir string, endHeight uint64) (string, string) {
	valueFileName := filepath.Join(evmStateGobDir, fmt.Sprintf("values-%d.gob", endHeight))
	allocatorFileName := filepath.Join(evmStateGobDir, fmt.Sprintf("allocators-%d.gob", endHeight))
	return valueFileName, allocatorFileName
}
