package pebble

import (
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/atree"

	"github.com/onflow/flow-go/fvm/evm/types"
)

var _ types.BackendStorage = &Register{}

type Register struct {
	store  *Storage
	height uint64
	batch  *pebble.Batch
}

// NewRegister creates a new index instance at the provided height, all reads and
// writes of the registers will happen at that height.
// this is not concurrency safe.
func NewRegister(
	store *Storage,
	height uint64,
	batch *pebble.Batch,
) *Register {
	return &Register{
		store:  store,
		height: height,
		batch:  batch,
	}
}

func (r *Register) GetValue(owner, key []byte) ([]byte, error) {
	var db pebble.Reader = r.store.db
	if r.batch != nil {
		db = r.batch
	}

	iter, err := db.NewIter(&pebble.IterOptions{
		LowerBound: r.idLower(owner, key),
		UpperBound: r.idUpper(owner, key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create register range iterator: %w", err)
	}
	defer func() {
		if err := iter.Close(); err != nil {
			r.store.log.Error().Err(err).Msg("failed to close register iterator")
		}
	}()

	found := iter.Last()
	if !found {
		// as per interface expectation we need to return nil if not found
		return nil, nil
	}

	val, err := iter.ValueAndErr()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get ledger value at owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return val, nil
}

func (r *Register) SetValue(owner, key, value []byte) error {
	id := r.id(owner, key)
	if err := r.store.set(ledgerValue, id, value, r.batch); err != nil {
		return fmt.Errorf(
			"failed to store ledger value for owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return nil
}

func (r *Register) ValueExists(owner, key []byte) (bool, error) {
	val, err := r.GetValue(owner, key)
	if err != nil {
		return false, err
	}

	return len(val) == 0, nil
}

func (r *Register) AllocateSlabIndex(_ []byte) (atree.SlabIndex, error) {
	return atree.SlabIndexUndefined, fmt.Errorf(
		"unexpected call to allocate slab index",
	)
}

// id calculates a ledger id with embedded block height for owner and key.
// The key for a register has the following schema:
// {owner}{key}{height}
func (r *Register) id(owner, key []byte) []byte {
	id := append(owner, key...)
	h := uint64Bytes(r.height)
	return append(id, h...)
}

func (r *Register) idUpper(owner, key []byte) []byte {
	id := []byte{ledgerValue}
	id = append(id, owner...)
	id = append(id, key...)
	// increase height +1 because upper bound is exclusive
	h := uint64Bytes(r.height + 1)
	return append(id, h...)
}

func (r *Register) idLower(owner, key []byte) []byte {
	id := []byte{ledgerValue}
	id = append(id, owner...)
	id = append(id, key...)
	// lower height is always 0
	return append(id, uint64Bytes(0)...)
}
