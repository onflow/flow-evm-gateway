package pebble

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/atree"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

var _ atree.Ledger = &Register{}

type Register struct {
	height uint64
	store  *Storage
	batch  *pebble.Batch
	mux    sync.RWMutex
}

// NewRegister creates a new index instance at the provided height, all reads and
// writes of the registers will happen at that height.
//
// Batch is an optional argument, if provided the operations will be performed
// inside that batch that later needs to be committed by the provider of the batch.
func NewRegister(store *Storage, height uint64, batch *pebble.Batch) *Register {
	return &Register{
		store:  store,
		height: height,
		batch:  batch,
		mux:    sync.RWMutex{},
	}
}

func (l *Register) GetValue(owner, key []byte) ([]byte, error) {
	l.mux.RLock()
	defer l.mux.RUnlock()

	var db pebble.Reader = l.store.db
	if l.batch != nil {
		db = l.batch
	}

	iter, err := db.NewIter(&pebble.IterOptions{
		LowerBound: l.idLower(owner, key),
		UpperBound: l.idUpper(owner, key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create register range itterator: %w", err)
	}
	defer func() {
		if err := iter.Close(); err != nil {
			if err != nil {
				l.store.log.Error().Err(err).Msg("failed to close register iterator")
			}
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

func (l *Register) SetValue(owner, key, value []byte) error {
	l.mux.Lock()
	defer l.mux.Unlock()

	id := l.id(owner, key)
	if err := l.store.set(ledgerValue, id, value, l.batch); err != nil {
		return fmt.Errorf(
			"failed to store ledger value for owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return nil
}

func (l *Register) ValueExists(owner, key []byte) (bool, error) {
	val, err := l.GetValue(owner, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (l *Register) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	l.mux.Lock()
	defer l.mux.Unlock()

	var index atree.SlabIndex

	val, err := l.store.batchGet(l.batch, ledgerSlabIndex, owner)
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
	if err := l.store.set(ledgerSlabIndex, owner, index[:], l.batch); err != nil {
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
func (l *Register) id(owner, key []byte) []byte {
	id := append(owner, key...)
	h := uint64Bytes(l.height)
	return append(id, h...)
}

func (l *Register) idUpper(owner, key []byte) []byte {
	id := []byte{ledgerValue}
	id = append(id, owner...)
	id = append(id, key...)
	// increase height +1 because upper bound is exclusive
	h := uint64Bytes(l.height + 1)
	return append(id, h...)
}

func (l *Register) idLower(owner, key []byte) []byte {
	id := []byte{ledgerValue}
	id = append(id, owner...)
	id = append(id, key...)
	// lower height is always 0
	return append(id, uint64Bytes(0)...)
}
