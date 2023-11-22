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
		mu:          sync.RWMutex{},
		blockHeight: 10,
	}
}

func (s *Store) LatestBlockHeight(ctx context.Context) (uint64, error) {
	return s.blockHeight, nil
}
