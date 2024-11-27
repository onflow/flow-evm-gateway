package main_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
	gethCommon "github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/offchain/storage"
	"github.com/onflow/flow-go/fvm/evm/testutils"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func TestStateDiff(t *testing.T) {

	height := uint64(218215348)

	// state1 := ExtractEVMState(t, flowGo.Testnet, "/var/flow52/evm/data/db", height)

	state1 := EVMStateFromReplayGobDir(t, "/var/flow/nov26_testnet_evm_state_gob", height)
	state2 := EVMStateFromCheckpointExtract(t, "/var/flow52/evm-state-from-checkpoint-218215348-migrated/")

	// state1 := ExtractEVMState(t, flowGo.Testnet,
	//      "/var/flow52/evm/data/db", uint64(228901661))
	// state2 := EVMStateFromCheckpointExtract(t, "/var/flow52/evm-state-from-checkpoint-228901661")

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

	ledger := storage.NewEphemeralStorage(storage.NewReadOnlyStorage(snapshot))
	bv, err := state.NewBaseView(ledger, storageRoot)
	require.NoError(t, err)

	evmState, err := state.Extract(storageRoot, bv)
	require.NoError(t, err)
	return evmState
}

func EVMStateFromCheckpointExtract(t *testing.T, dir string) *state.EVMState {
	enState, err := state.ImportEVMStateFromGob(dir)
	require.NoError(t, err)
	return enState
}

func EVMStateFromReplayGobDir(t *testing.T, gobDir string, flowHeight uint64) *state.EVMState {
	valueFileName, allocatorFileName := evmStateGobFileNamesByEndHeight(gobDir, flowHeight)
	chainID := flowGo.Testnet

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

func TestReadPebbleRootAccountStorageKeyID(t *testing.T) {
	chainID := flowGo.Testnet
	registerStoreDir := "/var/flow52/evm/data/db"
	flowHeight := uint64(228901661)

	store, err := pebble.New(registerStoreDir, log.Logger)
	require.NoError(t, err)

	storageRoot := evm.StorageAccountAddress(chainID)
	registerStore := pebble.NewRegisterStorage(store, storageRoot)
	regID := flowGo.RegisterID{
		Owner: string(storageRoot.Bytes()),
		Key:   state.AccountsStorageIDKey,
	}
	regVal, err := registerStore.Get(regID, flowHeight)
	require.NoError(t, err)

	require.NotNil(t, regVal)
	fmt.Printf("Register value: %x\n", regVal)

	_, err = registerStore.GetSnapshotAt(flowHeight)
	require.NoError(t, err)
}

func TestReadAccount(t *testing.T) {

	chainID := flowGo.Testnet
	registerStoreDir := "/var/flow52/evm/data/db"
	flowHeight := uint64(228901661)
	accountStr := "17DDB8C5aA1382Fb04d057e3ec42B65a43A5fF49"

	// Convert string to common.Address
	account := gethCommon.HexToAddress(accountStr)

	store, err := pebble.New(registerStoreDir, log.Logger)
	require.NoError(t, err)

	storageRoot := evm.StorageAccountAddress(chainID)
	registerStore := pebble.NewRegisterStorage(store, storageRoot)

	snapshot, err := registerStore.GetSnapshotAt(flowHeight)
	require.NoError(t, err)
	ledger := storage.NewEphemeralStorage(storage.NewReadOnlyStorage(snapshot))

	stateDB, err := state.NewStateDB(ledger, storageRoot)
	require.NoError(t, err)

	balance := stateDB.GetBalance(account)
	fmt.Println("balance: ", balance)
}
