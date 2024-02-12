package pebble

import (
	"encoding/binary"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go/fvm/evm/types"
	"sync"
)

var _ storage.BlockIndexer = &Blocks{}

type Blocks struct {
	store       *Storage
	mux         sync.RWMutex
	heightCache map[byte]uint64
}

func NewBlocks(store *Storage) *Blocks {
	return &Blocks{
		store:       store,
		mux:         sync.RWMutex{},
		heightCache: make(map[byte]uint64),
	}
}

func (b *Blocks) Store(block *types.Block) error {
	b.mux.Lock()
	defer b.mux.Unlock()

	val, err := block.ToBytes()
	if err != nil {
		return err
	}

	id, err := block.Hash()
	if err != nil {
		return err
	}

	// todo batch operations
	if err := b.store.set(blockHeightKey, uint64Key(block.Height), val); err != nil {
		return err
	}

	return b.store.set(blockIDKey, id.Bytes(), val)
}

func (b *Blocks) GetByHeight(height uint64) (*types.Block, error) {
	return b.getBlock(blockHeightKey, uint64Key(height))
}

func (b *Blocks) GetByID(ID common.Hash) (*types.Block, error) {
	return b.getBlock(blockIDKey, ID.Bytes())
}

func (b *Blocks) LatestHeight() (uint64, error) {
	return b.getHeight(latestHeightKey)
}

func (b *Blocks) FirstHeight() (uint64, error) {
	return b.getHeight(firstHeightKey)
}

func (b *Blocks) getBlock(keyCode byte, key []byte) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	data, err := b.store.get(keyCode, key)
	if err != nil {
		return nil, err
	}

	var block types.Block
	err = rlp.DecodeBytes(data, &block)
	if err != nil {
		return nil, err
	}

	return &block, nil
}

func (b *Blocks) getHeight(keyCode byte) (uint64, error) {
	if b.heightCache[keyCode] != 0 {
		return b.heightCache[keyCode], nil
	}

	val, err := b.store.get(keyCode)
	if err != nil {
		return 0, err
	}

	h := binary.BigEndian.Uint64(val)
	b.heightCache[keyCode] = h
	return h, nil
}
