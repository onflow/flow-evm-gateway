package pebble

import (
	"errors"
	"fmt"
	"sync"

	"github.com/onflow/atree"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

var _ atree.Ledger = &Ledger{}

// todo we need to support historic data,
// we likely need to create ledger with the context of block height
// and then prepend all keys with that height

type Ledger struct {
	height []byte
	store  *Storage
	mux    sync.RWMutex
}

func NewLedger(store *Storage, height uint64) *Ledger {
	return &Ledger{
		store:  store,
		height: uint64Bytes(height),
		mux:    sync.RWMutex{},
	}
}

func (l *Ledger) GetValue(owner, key []byte) ([]byte, error) {
	l.mux.RLock()
	defer l.mux.RUnlock()

	id := l.id(owner, key)
	val, err := l.store.get(ledgerValue, id)
	if err != nil {
		// as per interface expectation we need to remove nil if not found
		if errors.Is(err, errs.ErrEntityNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"failed to get ledger value at owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return val, nil
}

func (l *Ledger) SetValue(owner, key, value []byte) error {
	l.mux.Lock()
	defer l.mux.Unlock()

	id := l.id(owner, key)
	if err := l.store.set(ledgerValue, id, value, nil); err != nil {
		return fmt.Errorf(
			"failed to store ledger value for owner %x and key %x: %w",
			owner,
			key,
			err,
		)
	}

	return nil
}

func (l *Ledger) ValueExists(owner, key []byte) (bool, error) {
	val, err := l.GetValue(owner, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (l *Ledger) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	l.mux.Lock()
	defer l.mux.Unlock()

	var index atree.SlabIndex

	val, err := l.store.get(ledgerSlabIndex, owner)
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
	if err := l.store.set(ledgerSlabIndex, owner, index[:], nil); err != nil {
		return atree.SlabIndexUndefined, fmt.Errorf(
			"slab index failed to set for owner %x: %w",
			owner,
			err,
		)
	}

	return index, nil
}

// id calculate ledger id with included block height for owner and key
func (l *Ledger) id(owner, key []byte) []byte {
	id := append(l.height, owner...)
	id = append(id, key...)
	return id
}
