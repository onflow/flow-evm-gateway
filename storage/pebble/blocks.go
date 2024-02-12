package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var _ storage.BlockIndexer = &Blocks{}

type BlockOption func(block *Blocks) error

// WithInitHeight sets the first and last height to the provided value,
// this should be used to initialize an empty database, if the first and last
// heights are already set an error will be returned.
func WithInitHeight(height uint64) BlockOption {
	return func(block *Blocks) error {
		return block.storeInitHeight(height)
	}
}

type Blocks struct {
	store       *Storage
	mux         sync.RWMutex
	heightCache map[byte]uint64
}

func NewBlocks(store *Storage, opts ...BlockOption) (*Blocks, error) {
	blk := &Blocks{
		store:       store,
		mux:         sync.RWMutex{},
		heightCache: make(map[byte]uint64),
	}

	for _, opt := range opts {
		if err := opt(blk); err != nil {
			return nil, err
		}
	}

	return blk, nil
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
	if err := b.store.set(blockHeightKey, uint64Bytes(block.Height), val); err != nil {
		return err
	}

	// todo check if what is more often used block by id or block by height and fix accordingly if needed
	return b.store.set(blockIDHeightKey, id.Bytes(), uint64Bytes(block.Height))
}

func (b *Blocks) GetByHeight(height uint64) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	first, err := b.FirstHeight()
	if err != nil {
		return nil, err
	}

	last, err := b.LatestHeight()
	if err != nil {
		return nil, err
	}

	// check if the requested height is within the known range
	if height < first || height > last {
		return nil, errs.NotFound
	}

	return b.getBlock(blockHeightKey, uint64Bytes(height))
}

func (b *Blocks) GetByID(ID common.Hash) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	height, err := b.store.get(blockIDHeightKey)
	if err != nil {
		return nil, err
	}

	return b.getBlock(blockHeightKey, height)
}

func (b *Blocks) LatestHeight() (uint64, error) {
	return b.getHeight(latestHeightKey)
}

func (b *Blocks) FirstHeight() (uint64, error) {
	return b.getHeight(firstHeightKey)
}

func (b *Blocks) getBlock(keyCode byte, key []byte) (*types.Block, error) {
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
	b.mux.RLock()
	defer b.mux.RUnlock()

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

func (b *Blocks) storeInitHeight(height uint64) error {
	// check if first and last exists
	_, err := b.store.get(firstHeightKey)
	if !errors.Is(err, errs.NotFound) {
		return fmt.Errorf("can not overwrite an existing first height")
	}

	// todo batch
	if err := b.store.set(firstHeightKey, nil, uint64Bytes(height)); err != nil {
		return err
	}

	return b.store.set(latestHeightKey, nil, uint64Bytes(height))
}
