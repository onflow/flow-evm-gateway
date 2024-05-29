package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
)

var _ storage.BlockIndexer = &Blocks{}

type Blocks struct {
	store                *Storage
	mux                  sync.RWMutex
	latestEVMHeightCache uint64
}

func NewBlocks(store *Storage) *Blocks {
	return &Blocks{
		store:                store,
		mux:                  sync.RWMutex{},
		latestEVMHeightCache: 0,
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

	batch := b.store.newBatch()
	defer batch.Close()

	cadenceHeightBytes := uint64Bytes(cadenceHeight)
	evmHeightBytes := uint64Bytes(block.Height)

	if err := b.store.set(blockHeightKey, evmHeightBytes, val, batch); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	// todo check if what is more often used block by id or block by height and fix accordingly if needed
	if err := b.store.set(blockIDToHeightKey, id.Bytes(), evmHeightBytes, batch); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	// set latest height
	if err := b.store.set(evmHeightToCadenceHeightKey, evmHeightBytes, cadenceHeightBytes, batch); err != nil {
		return fmt.Errorf("failed to store evm to cadence height: %w", err)
	}

	if err := b.store.set(latestCadenceHeightKey, nil, cadenceHeightBytes, batch); err != nil {
		return fmt.Errorf("failed to store latest cadence height: %w", err)
	}

	if err := b.store.set(latestEVMHeightKey, nil, evmHeightBytes, batch); err != nil {
		return fmt.Errorf("failed to set latest evm height: %w", err)
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit block: %w", err)
	}

	b.latestEVMHeightCache = block.Height
	return nil
}

func (b *Blocks) GetByHeight(height uint64) (*types.Block, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	last, err := b.latestEVMHeight()
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

func (b *Blocks) GetHeightByID(ID common.Hash) (uint64, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	height, err := b.store.get(blockIDToHeightKey, ID.Bytes())
	if err != nil {
		return 0, fmt.Errorf("failed to get block by ID: %w", err)
	}

	return binary.BigEndian.Uint64(height), nil
}

func (b *Blocks) LatestEVMHeight() (uint64, error) {
	b.mux.RLock()
	defer b.mux.RUnlock()

	return b.latestEVMHeight()
}

func (b *Blocks) latestEVMHeight() (uint64, error) {
	if b.latestEVMHeightCache != 0 {
		return b.latestEVMHeightCache, nil
	}

	val, err := b.store.get(latestEVMHeightKey)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return 0, errs.ErrNotInitialized
		}
		return 0, fmt.Errorf("failed to get height: %w", err)
	}

	h := binary.BigEndian.Uint64(val)
	b.latestEVMHeightCache = h
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

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(height), nil); err != nil {
		return fmt.Errorf("failed to store latest cadence height: %w", err)
	}

	return nil
}

// InitHeights sets the Cadence height to zero as well as EVM heights. Used for empty database init.
func (b *Blocks) InitHeights(cadenceHeight uint64) error {
	// sanity check, make sure we don't have any heights stored, disable overwriting the database
	_, err := b.LatestEVMHeight()
	if !errors.Is(err, errs.ErrNotInitialized) {
		return fmt.Errorf("can not init the database that already has data stored")
	}

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(cadenceHeight), nil); err != nil {
		return fmt.Errorf("failed to set init Cadence height: %w", err)
	}

	if err := b.store.set(latestEVMHeightKey, nil, uint64Bytes(0), nil); err != nil {
		return fmt.Errorf("failed to set init EVM height: %w", err)
	}

	// we store genesis block because it isn't emitted over the network
	if err := b.Store(cadenceHeight, types.GenesisBlock); err != nil {
		return fmt.Errorf("faield to set init genesis block: %w", err)
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
