package storage

import (
	"fmt"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/environment"
	"github.com/onflow/flow-go/fvm/errors"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
)

var _ types.BackendStorage = &RegisterDelta{}
var _ types.BackendStorageSnapshot = &RegisterDelta{}

type RegisterDelta struct {
	deltas    map[flow.RegisterID]flow.RegisterEntry
	registers types.BackendStorageSnapshot
}

// NewRegisterDelta creates a new instance of RegisterDelta. It is not concurrency safe.
// This allows for the caller to build new state on top of the provided snapshot.
// The new state is not persisted. The caller is responsible for persisting the state using
// the `GetUpdates` method.
func NewRegisterDelta(
	registers types.BackendStorageSnapshot,
) *RegisterDelta {
	return &RegisterDelta{
		deltas:    make(map[flow.RegisterID]flow.RegisterEntry),
		registers: registers,
	}
}

// GetValue gets the value for the given register ID. If the value was set, it returns that value.
// If the value was not set, it reads the value from the snapshot.
func (r *RegisterDelta) GetValue(owner []byte, key []byte) ([]byte, error) {
	id := flow.CadenceRegisterID(owner, key)

	// get from delta first
	if delta, ok := r.deltas[id]; ok {
		return delta.Value, nil
	}

	// get from storage
	return r.registers.GetValue(owner, key)
}

// SetValue sets the value for the given register ID. It sets it in the delta, not in the storage.
func (r *RegisterDelta) SetValue(owner, key, value []byte) error {
	id := flow.CadenceRegisterID(owner, key)

	r.deltas[id] = flow.RegisterEntry{Key: id, Value: value}

	return nil
}

func (r *RegisterDelta) ValueExists(owner []byte, key []byte) (bool, error) {
	value, err := r.GetValue(owner, key)
	if err != nil {
		return false, err
	}
	return len(value) > 0, nil
}

// GetUpdates returns the register updates from the delta to be applied to storage.
func (r *RegisterDelta) GetUpdates() flow.RegisterEntries {
	entries := make(flow.RegisterEntries, 0, len(r.deltas))
	for id, delta := range r.deltas {
		entries = append(entries, flow.RegisterEntry{Key: id, Value: delta.Value})
	}

	return entries
}

func (r *RegisterDelta) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	return allocateSlabIndex(owner, r)

}

// allocateSlabIndex allocates a new slab index for the given owner and key.
// this method only uses the storage get/set methods.
func allocateSlabIndex(owner []byte, storage types.BackendStorage) (atree.SlabIndex, error) {
	// get status
	address := flow.BytesToAddress(owner)
	id := flow.AccountStatusRegisterID(address)
	statusBytes, err := storage.GetValue(owner, []byte(id.Key))
	if err != nil {
		return atree.SlabIndex{}, fmt.Errorf(
			"failed to load account status for the account (%s): %w",
			address.String(),
			err)
	}
	if len(statusBytes) == 0 {
		return atree.SlabIndex{}, errors.NewAccountNotFoundError(address)
	}
	status, err := environment.AccountStatusFromBytes(statusBytes)
	if err != nil {
		return atree.SlabIndex{}, err
	}

	// get and increment the index
	index := status.SlabIndex()
	newIndexBytes := index.Next()

	// store nil so that the setValue for new allocated slabs would be faster
	// and won't do ledger getValue for every new slabs (currently happening to
	// compute storage size changes)
	// this way the getValue would load this value from deltas
	key := atree.SlabIndexToLedgerKey(index)
	err = storage.SetValue(owner, key, []byte{})
	if err != nil {
		return atree.SlabIndex{}, fmt.Errorf(
			"failed to allocate an storage index: %w",
			err)
	}

	// update the storageIndex bytes
	status.SetStorageIndex(newIndexBytes)

	err = storage.SetValue(owner, []byte(id.Key), status.ToBytes())
	if err != nil {
		return atree.SlabIndex{}, fmt.Errorf(
			"failed to store the account status for account (%s): %w",
			address.String(),
			err)
	}
	return index, nil

}
