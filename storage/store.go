package storage

import (
	"context"
	"sync"
)

type Store struct {
	mu           sync.RWMutex
	latestHeight uint64
}

// NewStore returns a new in-memory Store implementation.
// TODO(m-Peter): If `LatestBlockHeight` is called before,
// `StoreBlockHeight`, the called will receive 0. To avoid
// this race condition, we should require an initial value for
// `latestHeight` in `NewStore`.
func NewStore() *Store {
	return &Store{}
}

func (s *Store) LatestBlockHeight(ctx context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.latestHeight, nil
}

func (s *Store) StoreBlockHeight(ctx context.Context, blockHeight uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.storeBlockHeight(blockHeight)
}

func (s *Store) storeBlockHeight(blockHeight uint64) error {
	if blockHeight > s.latestHeight {
		s.latestHeight = blockHeight
	}

	return nil
}
