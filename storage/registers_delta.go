package storage

import (
	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
)

var _ types.BackendStorage = &RegistersDelta{}

type RegisterValueAtHeight interface {
	Get(id flow.RegisterID, height uint64) (flow.RegisterValue, error)
}

// RegistersDelta holds the register changes for a current height.
// It is used to collect register changes for a block, while also serving register data
// for the previous heights.
// once all changes were collected, use the `GetUpdates` method to get the register updates.
// and apply them to storage.
// Not safe for concurrent usage.
//
// To avoid creating a new instance of RegistersDelta for every block, use the `Reset` method.
type RegistersDelta struct {
	// the cache is used to cache register reads for the current height.
	cache map[flow.RegisterID]flow.RegisterValue

	// deltas is a map of register IDs to their respective deltas.
	deltas map[flow.RegisterID]flow.RegisterEntry
	// height is the height at which the deltas were applied.
	height uint64

	registers RegisterValueAtHeight
}

// NewRegistersDelta creates a new instance of RegistersDelta.
// height is used for `GetValue` to fetch the register value at the given height.
// height is not checked against the latest register height. The caller is responsible
// for ensuring that the height is sequential.
func NewRegistersDelta(
	height uint64,
	registers RegisterValueAtHeight,
) *RegistersDelta {
	return &RegistersDelta{
		cache:     make(map[flow.RegisterID]flow.RegisterValue),
		deltas:    make(map[flow.RegisterID]flow.RegisterEntry),
		height:    height,
		registers: registers,
	}
}

func (r *RegistersDelta) GetValue(owner []byte, key []byte) ([]byte, error) {
	id := flow.CadenceRegisterID(owner, key)

	// get from delta first
	if delta, ok := r.deltas[id]; ok {
		return delta.Value, nil
	}

	// get from cache if not found in delta
	if value, ok := r.cache[id]; ok {
		return value, nil
	}

	// get from storage
	value, err := r.registers.Get(id, r.height)
	if err != nil {
		return nil, err
	}

	r.cache[id] = value
	return value, nil
}

func (r *RegistersDelta) SetValue(owner, key, value []byte) error {
	id := flow.CadenceRegisterID(owner, key)

	r.deltas[id] = flow.RegisterEntry{Key: id, Value: value}

	return nil
}

func (r *RegistersDelta) ValueExists(owner []byte, key []byte) (bool, error) {
	value, err := r.GetValue(owner, key)
	if err != nil {
		return false, err
	}
	return len(value) > 0, nil
}

// GetUpdates returns the register updates for the current height to be applied to storage.
func (r *RegistersDelta) GetUpdates() flow.RegisterEntries {
	entries := make(flow.RegisterEntries, 0, len(r.deltas))
	for id, delta := range r.deltas {
		entries = append(entries, flow.RegisterEntry{Key: id, Value: delta.Value})
	}

	return entries
}

// Reset resets the state of the registers delta to the provided height.
// This can be used to as an optimization to avoid creating a new instance of RegistersDelta
// for every block.
func (r *RegistersDelta) Reset(height uint64) {
	r.height = height
	clear(r.deltas)
	clear(r.cache)
}

func (r *RegistersDelta) AllocateSlabIndex(_ []byte) (atree.SlabIndex, error) {
	// TODO: If needed add later
	panic("should not be called")
}
