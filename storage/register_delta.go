package storage

import (
	"github.com/onflow/atree"
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

func (r *RegisterDelta) AllocateSlabIndex(_ []byte) (atree.SlabIndex, error) {
	return atree.SlabIndex{}, nil
}
