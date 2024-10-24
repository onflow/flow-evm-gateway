package pebble

import (
	"errors"
	"fmt"
	"sync"

	"github.com/onflow/atree"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

var _ atree.Ledger = &BlockMetadata{}

type BlockMetadata struct {
	store *Storage
	mux   sync.RWMutex
}

func NewBlockMetadata(store *Storage) *BlockMetadata {
	return &BlockMetadata{
		store: store,
		mux:   sync.RWMutex{},
	}
}

func (l *BlockMetadata) GetValue(owner, key []byte) ([]byte, error) {
	l.mux.RLock()
	defer l.mux.RUnlock()

	id := append(owner, key...)
	val, err := l.store.get(bmValue, id)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get block metadata value at owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return val, nil
}

func (l *BlockMetadata) SetValue(owner, key, value []byte) error {
	l.mux.Lock()
	defer l.mux.Unlock()

	id := append(owner, key...)
	if err := l.store.set(bmValue, id, value, nil); err != nil {
		return fmt.Errorf(
			"failed to store block metadata value for owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return nil
}

func (l *BlockMetadata) ValueExists(owner, key []byte) (bool, error) {
	val, err := l.GetValue(owner, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (l *BlockMetadata) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	l.mux.Lock()
	defer l.mux.Unlock()

	var index atree.SlabIndex

	val, err := l.store.get(bmSlabIndex, owner)
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

	index.Next()
	if err := l.store.set(bmSlabIndex, owner, index[:], nil); err != nil {
		return atree.SlabIndexUndefined, fmt.Errorf(
			"slab index failed to set for owner %x: %w",
			owner,
			err,
		)
	}

	return index, nil
}
