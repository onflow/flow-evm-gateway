package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk"
	flowGo "github.com/onflow/flow-go/model/flow"

	"github.com/ethereum/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
)

var testnetBrokenParentHashBlockHeights = []uint64{
	1,
	1385491,
}

var _ storage.BlockIndexer = &Blocks{}

type Blocks struct {
	store   *Storage
	chainID flowGo.ChainID
}

func NewBlocks(store *Storage, chainID flowGo.ChainID) *Blocks {
	return &Blocks{
		store:   store,
		chainID: chainID,
	}
}

func (b *Blocks) Store(
	cadenceHeight uint64,
	cadenceID flow.Identifier,
	block *models.Block,
	batch *pebble.Batch,
) error {
	// dev note: please be careful if any store reads are added here,
	// store.batchGet must be used instead and batch must be used

	val, err := block.ToBytes()
	if err != nil {
		return err
	}

	id, err := block.Hash()
	if err != nil {
		return err
	}

	cadenceHeightBytes := uint64Bytes(cadenceHeight)
	evmHeightBytes := uint64Bytes(block.Height)

	if err := b.store.set(blockHeightKey, evmHeightBytes, val, batch); err != nil {
		return fmt.Errorf(
			"failed to store EVM block: %d at Cadence height: %d, with: %w",
			block.Height,
			cadenceHeight,
			err,
		)
	}

	// todo check if what is more often used block by id or block by height and fix accordingly if needed
	if err := b.store.set(blockIDToHeightKey, id.Bytes(), evmHeightBytes, batch); err != nil {
		return fmt.Errorf(
			"failed to store EVM block ID: %s to height: %d mapping, with: %w",
			id,
			block.Height,
			err,
		)
	}

	// set mapping of evm height to cadence block height
	if err := b.store.set(evmHeightToCadenceHeightKey, evmHeightBytes, cadenceHeightBytes, batch); err != nil {
		return fmt.Errorf(
			"failed to store EVM block height: %d to Cadence height: %d mapping, with: %w",
			block.Height,
			cadenceHeight,
			err,
		)
	}

	// set mapping of evm height to cadence block id
	if err := b.store.set(evmHeightToCadenceIDKey, evmHeightBytes, cadenceID.Bytes(), batch); err != nil {
		return fmt.Errorf(
			"failed to store EVM block height: %d to Cadence ID: %s mapping, with: %w",
			block.Height,
			cadenceID,
			err,
		)
	}

	if err := b.store.set(latestCadenceHeightKey, nil, cadenceHeightBytes, batch); err != nil {
		return fmt.Errorf(
			"failed to store latest Cadence height: %d, with: %w",
			cadenceHeight,
			err,
		)
	}

	if err := b.store.set(latestEVMHeightKey, nil, evmHeightBytes, batch); err != nil {
		return fmt.Errorf(
			"failed to store latest EVM height: %d, with: %w",
			block.Height,
			err,
		)
	}

	return nil
}

func (b *Blocks) GetByHeight(height uint64) (*models.Block, error) {
	last, err := b.latestEVMHeight()
	if err != nil {
		return nil, err
	}

	// check if the requested height is within the known range
	if height > last {
		return nil, errs.ErrEntityNotFound
	}

	blk, err := b.getBlock(blockHeightKey, uint64Bytes(height))
	if err != nil {
		return nil, fmt.Errorf("failed to get EVM block by height: %d, with: %w", height, err)
	}

	return blk, nil
}

func (b *Blocks) GetByID(ID common.Hash) (*models.Block, error) {
	height, err := b.store.get(blockIDToHeightKey, ID.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to get EVM block by ID: %s, with: %w", ID, err)
	}

	blk, err := b.getBlock(blockHeightKey, height)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get EVM block by height: %d, with: %w",
			binary.BigEndian.Uint64(height),
			err,
		)
	}

	return blk, nil
}

func (b *Blocks) GetHeightByID(ID common.Hash) (uint64, error) {
	height, err := b.store.get(blockIDToHeightKey, ID.Bytes())
	if err != nil {
		return 0, fmt.Errorf("failed to get EVM block by ID: %s, with: %w", ID, err)
	}

	return binary.BigEndian.Uint64(height), nil
}

func (b *Blocks) LatestEVMHeight() (uint64, error) {
	return b.latestEVMHeight()
}

func (b *Blocks) latestEVMHeight() (uint64, error) {
	val, err := b.store.get(latestEVMHeightKey)
	if err != nil {
		if errors.Is(err, errs.ErrEntityNotFound) {
			return 0, errs.ErrStorageNotInitialized
		}
		return 0, fmt.Errorf("failed to get EVM height: %w", err)
	}

	return binary.BigEndian.Uint64(val), nil
}

func (b *Blocks) LatestCadenceHeight() (uint64, error) {
	val, err := b.store.get(latestCadenceHeightKey)
	if err != nil {
		if errors.Is(err, errs.ErrEntityNotFound) {
			return 0, errs.ErrStorageNotInitialized
		}
		return 0, fmt.Errorf("failed to get latest Cadence height: %w", err)
	}

	return binary.BigEndian.Uint64(val), nil
}

func (b *Blocks) SetLatestCadenceHeight(height uint64, batch *pebble.Batch) error {
	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(height), batch); err != nil {
		return fmt.Errorf("failed to store latest Cadence height: %d, with: %w", height, err)
	}

	return nil
}

// InitHeights sets the Cadence height to zero as well as EVM heights. Used for empty database init.
func (b *Blocks) InitHeights(cadenceHeight uint64, cadenceID flow.Identifier, batch *pebble.Batch) error {
	// sanity check, make sure we don't have any heights stored, disable overwriting the database
	_, err := b.LatestEVMHeight()
	if !errors.Is(err, errs.ErrStorageNotInitialized) {
		return fmt.Errorf("can't init the database that already has data stored")
	}

	if err := b.store.set(latestCadenceHeightKey, nil, uint64Bytes(cadenceHeight), batch); err != nil {
		return fmt.Errorf("failed to init latest Cadence height at: %d, with: %w", cadenceHeight, err)
	}

	if err := b.store.set(latestEVMHeightKey, nil, uint64Bytes(0), batch); err != nil {
		return fmt.Errorf("failed to init latest EVM height at: %d, with: %w", 0, err)
	}

	// we store genesis block because it isn't emitted over the network
	genesisBlock := models.GenesisBlock(b.chainID)
	if err := b.Store(cadenceHeight, cadenceID, genesisBlock, batch); err != nil {
		return fmt.Errorf("failed to store genesis block at Cadence height: %d, with: %w", cadenceHeight, err)
	}

	return nil
}

func (b *Blocks) GetCadenceHeight(evmHeight uint64) (uint64, error) {
	val, err := b.store.get(evmHeightToCadenceHeightKey, uint64Bytes(evmHeight))
	if err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint64(val), nil
}

func (b *Blocks) GetCadenceID(evmHeight uint64) (flow.Identifier, error) {
	val, err := b.store.get(evmHeightToCadenceIDKey, uint64Bytes(evmHeight))
	if err != nil {
		return flow.Identifier{}, err
	}

	return flow.BytesToID(val), nil
}

func (b *Blocks) getBlock(keyCode byte, key []byte) (*models.Block, error) {
	data, err := b.store.get(keyCode, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	block, err := models.NewBlockFromBytes(data)
	if err != nil {
		return nil, err
	}

	if b.chainID == flowGo.Testnet && slices.Contains(testnetBrokenParentHashBlockHeights, block.Height) {
		// Since we are going to modify the `block.ParentBlockHash` field,
		// we need to set the `block.FixedHash` field. If we don't do so,
		// `block.Hash()` will return a different hash.
		blockHash, err := block.Hash()
		if err != nil {
			return nil, err
		}
		block.FixedHash = blockHash

		parentBlock, err := b.getBlock(blockHeightKey, uint64Bytes(block.Height-1))
		if err != nil {
			return nil, err
		}
		// Due to the breaking change of the block hash calculation, after the
		// introduction of the `PrevRandao` field, we need to manually set the
		// `ParentBlockHash` field, for the 2 affected blocks on `testnet`
		// network. `mainnet` was not affected by this.
		block.ParentBlockHash, err = parentBlock.Hash()
		if err != nil {
			return nil, err
		}
	}

	return block, nil
}
