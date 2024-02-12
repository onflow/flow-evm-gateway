package pebble

import (
	"fmt"
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/flow-go/fvm/evm/types"
	"io"
)

type Storage struct {
	db *pebble.DB
}

func (s *Storage) storeBlock(block *types.Block) error {
	key := []byte(fmt.Sprintf("blocks/%d", block.Height))
	val, err := block.ToBytes()
	if err != nil {
		return err
	}

	// we make the sync false since we can always repeat the indexing in case of crash which should be rare so the performance optimization is worth it
	return s.db.Set(key, val, &pebble.WriteOptions{Sync: false})
}

func (s *Storage) getBlock(height uint64) (*types.Block, error) {
	key := []byte(fmt.Sprintf("blocks/%d", height))
	data, closer, err := s.db.Get(key)
	if err != nil {
		return nil, err
	}
	defer func(closer io.Closer) {
		err = closer.Close()
		if err != nil {

		}
	}(closer)

	var block types.Block
	err = rlp.DecodeBytes(data, &block)
	if err != nil {
		return nil, err
	}

	return &block, nil
}
