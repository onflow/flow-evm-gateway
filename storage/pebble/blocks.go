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

// default initCadenceHeight for initializing the database, we don't use 0 as it has
// a special meaning to represent latest block in the AN API context.
const initCadenceHeight = uint64(1)

var _ storage.BlockIndexer = &Blocks{}

type Blocks struct {
	store           *Storage
	mux             sync.RWMutex
	latestEVMHeight uint64
}

func NewBlocks(store *Storage) *Blocks {
	return &Blocks{
		store:           store,
		mux:             sync.RWMutex{},
		latestEVMHeight: 0,
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

	return b.setLastHeight(cadenceHeight, block.Height)
}

func (b *Blocks) GetByHeight(height uint64) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	last, err := b.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	// check if the requested height is within the known range
	if height > last {
		return nil, errs.ErrNotFound
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
	b.mux.RLock()
	defer b.mux.RUnlock()

	if b.latestEVMHeight != 0 {
		return b.latestEVMHeight, nil
	}

	val, err := b.store.get(latestEVMHeightKey)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return 0, errs.ErrNotInitialized
		}
		return 0, fmt.Errorf("failed to get height: %w", err)
	}

	h := binary.BigEndian.Uint64(val)
	b.latestEVMHeight = h
	return h, nil
}

func (b *Blocks) LatestCadenceHeight() (uint64, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	val, err := b.store.get(latestCadenceHeightKey)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return 0, errs.ErrNotInitialized
		}
		return 0, fmt.Errorf("failed to get latest cadence height: %w", err)
	}

	return binary.BigEndian.Uint64(val), nil
}

func (b *Blocks) SetLatestCadenceHeight(height uint64) error {
	b.mux.Lock()
	defer b.mux.Unlock()

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(height)); err != nil {
		return fmt.Errorf("failed to store latest cadence height: %w", err)
	}

	return nil
}

// InitHeights sets the Cadence height to zero as well as EVM heights. Used for empty database init.
func (b *Blocks) InitHeights() error {
	// sanity check, make sure we don't have any heights stored, disable overwriting the database
	_, err := b.LatestEVMHeight()
	if !errors.Is(err, errs.ErrNotInitialized) {
		return fmt.Errorf("can not init the database that already has data stored")
	}

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(initCadenceHeight)); err != nil {
		return fmt.Errorf("failed to set init Cadence height: %w", err)
	}

	if err := b.store.set(latestEVMHeightKey, nil, uint64Bytes(0)); err != nil {
		return fmt.Errorf("failed to set init EVM height: %w", err)
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

func (b *Blocks) setLastHeight(cadenceHeight, evmHeight uint64) error {
	if err := b.store.set(evmHeightToCadenceHeightKey, uint64Bytes(evmHeight), uint64Bytes(cadenceHeight)); err != nil {
		return fmt.Errorf("failed to store evm to cadence height: %w", err)
	}

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(cadenceHeight)); err != nil {
		return fmt.Errorf("failed to store latest cadence height: %w", err)
	}

	if err := b.store.set(latestEVMHeightKey, nil, uint64Bytes(evmHeight)); err != nil {
		return fmt.Errorf("failed to set latest evm height: %w", err)
	}

	b.latestEVMHeight = evmHeight
	return nil
}
