package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var _ storage.BlockIndexer = &Blocks{}

type Blocks struct {
	store *Storage
	mux   sync.RWMutex
	// todo LRU caching with size limit
	heightCache map[byte]uint64
}

func NewBlocks(store *Storage) *Blocks {
	return &Blocks{
		store:       store,
		mux:         sync.RWMutex{},
		heightCache: make(map[byte]uint64),
	}
}

func (b *Blocks) Store(cadenceHeight uint64, block *types.Block) error {
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
	evmHeight := uint64Bytes(block.Height)
	if err := b.store.set(blockHeightKey, evmHeight, val); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	// todo check if what is more often used block by id or block by height and fix accordingly if needed
	if err := b.store.set(blockIDToHeightKey, id.Bytes(), evmHeight); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	if err := b.store.set(evmHeightToCadenceHeightKey, evmHeight, uint64Bytes(cadenceHeight)); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(cadenceHeight)); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	return b.setLastHeight(block.Height)
}

func (b *Blocks) GetByHeight(height uint64) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	first, err := b.FirstEVMHeight()
	if err != nil {
		return nil, err
	}

	last, err := b.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	// check if the requested height is within the known range
	if height < first || height > last {
		return nil, errs.NotFound
	}

	blk, err := b.getBlock(blockHeightKey, uint64Bytes(height))
	if err != nil {
		return nil, fmt.Errorf("failed to get block by height: %w", err)
	}

	return blk, nil
}

func (b *Blocks) GetByID(ID common.Hash) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	height, err := b.store.get(blockIDToHeightKey, ID.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to get block by ID: %w", err)
	}

	blk, err := b.getBlock(blockHeightKey, height)
	if err != nil {
		return nil, fmt.Errorf("failed to get block by height: %w", err)
	}

	return blk, nil
}

func (b *Blocks) LatestEVMHeight() (uint64, error) {
	return b.getHeight(latestEVMHeightKey)
}

func (b *Blocks) FirstEVMHeight() (uint64, error) {
	return b.getHeight(firstEVMHeightKey)
}

func (b *Blocks) LatestCadenceHeight() (uint64, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	val, err := b.store.get(latestCadenceHeightKey)
	if err != nil {
		if errors.Is(err, errs.NotFound) {
			return 0, errs.NotInitialized
		}
		return 0, fmt.Errorf("failed to get latest cadence height: %w", err)
	}

	return binary.BigEndian.Uint64(val), nil
}

// InitCadenceHeight sets the latest Cadence height
func (b *Blocks) InitCadenceHeight(height uint64) error {
	err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(height))
	if err != nil {
		return fmt.Errorf("failed to set latest Cadence height: %w", err)
	}

	return nil
}

func (b *Blocks) GetCadenceHeight(evmHeight uint64) (uint64, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	val, err := b.store.get(evmHeightToCadenceHeightKey, uint64Bytes(evmHeight))
	if err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint64(val), nil
}

func (b *Blocks) getBlock(keyCode byte, key []byte) (*types.Block, error) {
	data, err := b.store.get(keyCode, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return types.NewBlockFromBytes(data)
}

func (b *Blocks) setLastHeight(height uint64) error {
	err := b.store.set(latestEVMHeightKey, nil, uint64Bytes(height))
	if err != nil {
		return fmt.Errorf("failed to set latest block height: %w", err)
	}
	// update cache
	b.heightCache[latestEVMHeightKey] = height
	return nil
}

func (b *Blocks) getHeight(keyCode byte) (uint64, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	if b.heightCache[keyCode] != 0 {
		return b.heightCache[keyCode], nil
	}

	val, err := b.store.get(keyCode)
	if err != nil {
		if errors.Is(err, errs.NotFound) {
			return 0, errs.NotInitialized
		}
		return 0, fmt.Errorf("failed to get height: %w", err)
	}

	h := binary.BigEndian.Uint64(val)
	b.heightCache[keyCode] = h
	return h, nil
}
