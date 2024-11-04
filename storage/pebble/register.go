package pebble

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/atree"

	errs "github.com/onflow/flow-evm-gateway/models/errors"

	"github.com/onflow/flow-go/fvm/evm/types"
)

var _ atree.Ledger = &Register{}
var _ types.StorageProvider = &Register{}

type Register struct {
	store  *Storage
	height uint64
	batch  *pebble.Batch
	mux    sync.RWMutex
}

// NewRegister creates a new index instance at the provided height, all reads and
// writes of the registers will happen at that height.
func NewRegister(store *Storage, height uint64, batch *pebble.Batch) *Register {
	return &Register{
		store:  store,
		height: height,
		batch:  batch,
		mux:    sync.RWMutex{},
	}
}

func (r *Register) GetSnapshotAt(evmBlockHeight uint64) (types.BackendStorageSnapshot, error) {
	return &Register{
		store:  r.store,
		height: evmBlockHeight,
		mux:    sync.RWMutex{},
	}, nil
}

func (r *Register) GetValue(owner, key []byte) ([]byte, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

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
	r.mux.Lock()
	defer r.mux.Unlock()

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

	return val != nil, nil
}

func (r *Register) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	r.mux.Lock()
	defer r.mux.Unlock()

	var index atree.SlabIndex

	val, err := r.store.batchGet(r.batch, ledgerSlabIndex, owner)
	if err != nil {
		if !errors.Is(err, errs.ErrEntityNotFound) {
			return atree.SlabIndexUndefined, err
		}
	}

	if val != nil {
		if len(val) != len(index) {
			return atree.SlabIndexUndefined, fmt.Errorf(
				"slab index was not stored in correct format for owner %x",
				owner,
			)
		}

		copy(index[:], val)
	}

	index = index.Next()
	if err := r.store.set(ledgerSlabIndex, owner, index[:], r.batch); err != nil {
		return atree.SlabIndexUndefined, fmt.Errorf(
			"slab index failed to set for owner %x: %w",
			owner,
			err,
		)
	}

	return index, nil
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
