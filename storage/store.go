package storage

import (
	"context"
	"sync"
)

type Store struct {
	mu sync.RWMutex
	// highest block height
	blockHeight uint64
}

// New returns a new in-memory Store implementation.
func NewStore() *Store {
	return &Store{
		mu: sync.RWMutex{},
	}
}

func (s *Store) LatestBlockHeight(ctx context.Context) (uint64, error) {
	return s.blockHeight, nil
}

func (s *Store) StoreBlockHeight(ctx context.Context, blockHeight uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.storeBlockHeight(blockHeight)
}

func (s *Store) storeBlockHeight(blockHeight uint64) error {
	if blockHeight > s.blockHeight {
		s.blockHeight = blockHeight
	}

	return nil
}
