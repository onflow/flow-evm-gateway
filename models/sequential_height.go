package models

import (
	"errors"
	"sync/atomic"
)

var InvalidHeightErr = errors.New("invalid height")

// SequentialHeight tracks a block height and enforces rules about
// the valid next height.
type SequentialHeight struct {
	height atomic.Uint64
}

func NewSequentialHeight(init uint64) *SequentialHeight {
	h := &SequentialHeight{}
	h.height.Store(init)

	return h
}

// Increment the height value according to the rules.
// A valid next height must be either incremented
// by one, or must be the same as previous height to make the action idempotent.
// Expected errors:
// if the height is not incremented according to the rules a InvalidHeightErr error is returned
func (s *SequentialHeight) Increment(nextHeight uint64) error {
	for {
		current := s.height.Load()
		if nextHeight-current > 1 {
			return InvalidHeightErr
		}

		if s.height.CompareAndSwap(current, nextHeight) {
			return nil
		}
	}
}

func (s *SequentialHeight) Load() uint64 {
	return s.height.Load()
}
