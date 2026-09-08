package pebble

import (
	"encoding/binary"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/rs/zerolog"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

const (
	// TruncateBatchSize is the number of blocks to delete in a single batch.
	// Processing in batches ensures:
	// 1. Memory efficiency - don't load too much data at once
	// 2. Consistency - if interrupted, latestEVMHeight marker indicates valid data boundary
	TruncateBatchSize = 100
)

// TruncateStorage handles truncating all storage data above a given EVM height.
// This is used when re-indexing from a past height to ensure the database
// is in a consistent state.
//
// The truncation is performed backwards (from highest to lowest height) to ensure
// database consistency if the operation is interrupted. After each batch,
// the latestEVMHeight is updated to reflect the new boundary.
type TruncateStorage struct {
	store    *Storage
	blocks   *Blocks
	receipts *Receipts
	traces   *Traces
	log      zerolog.Logger
}

// NewTruncateStorage creates a new TruncateStorage instance.
func NewTruncateStorage(
	store *Storage,
	blocks *Blocks,
	receipts *Receipts,
	traces *Traces,
	log zerolog.Logger,
) *TruncateStorage {
	return &TruncateStorage{
		store:    store,
		blocks:   blocks,
		receipts: receipts,
		traces:   traces,
		log:      log.With().Str("component", "truncate").Logger(),
	}
}

// Truncate removes all data above the specified EVM height.
// It processes blocks backwards in batches of TruncateBatchSize.
//
// The method:
// 1. Gets the current latest EVM height
// 2. For each batch (processing backwards from latest to target):
//   - Deletes block data, receipts, blooms, transactions, and traces
//   - Updates latestEVMHeight to the lowest height in batch - 1
//   - Commits the batch
//
// 3. Updates latestCadenceHeight to match the target EVM block's Cadence height
//
// If interrupted, the database remains consistent because:
// - Data below latestEVMHeight is guaranteed to be complete
// - Data above latestEVMHeight may be partially deleted but will be re-indexed
func (t *TruncateStorage) Truncate(targetEVMHeight uint64) error {
	latestEVMHeight, err := t.blocks.LatestEVMHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest EVM height: %w", err)
	}

	// Nothing to truncate
	if latestEVMHeight <= targetEVMHeight {
		t.log.Info().
			Uint64("latest-evm-height", latestEVMHeight).
			Uint64("target-evm-height", targetEVMHeight).
			Msg("no truncation needed, latest height is at or below target")
		return nil
	}

	t.log.Info().
		Uint64("from-height", latestEVMHeight).
		Uint64("to-height", targetEVMHeight).
		Msg("starting storage truncation")

	// Process backwards in batches
	// because it ensures that if the process is interrupted, the latestEVMHeight marker can still
	// indicate a valid boundary of complete data.
	currentHeight := latestEVMHeight
	for currentHeight > targetEVMHeight {
		// Calculate batch range (process backwards)
		batchEnd := currentHeight
		var batchStart uint64
		if currentHeight >= TruncateBatchSize+targetEVMHeight {
			batchStart = currentHeight - TruncateBatchSize + 1
		} else {
			batchStart = targetEVMHeight + 1
		}

		t.log.Info().
			Uint64("batch-start", batchStart).
			Uint64("batch-end", batchEnd).
			Msg("processing truncation batch")

		if err := t.truncateBatch(batchStart, batchEnd); err != nil {
			return fmt.Errorf("failed to truncate batch [%d, %d]: %w", batchStart, batchEnd, err)
		}

		currentHeight = batchStart - 1
	}

	// Update latestCadenceHeight to match the target EVM block
	cadenceHeight, err := t.blocks.GetCadenceHeight(targetEVMHeight)
	if err != nil {
		return fmt.Errorf("failed to get Cadence height for EVM height %d: %w", targetEVMHeight, err)
	}

	batch := t.store.NewBatch()
	defer func() {
		if err := batch.Close(); err != nil {
			t.log.Error().Err(err).Msg("failed to close batch")
		}
	}()

	if err := t.blocks.SetLatestCadenceHeight(cadenceHeight, batch); err != nil {
		return fmt.Errorf("failed to set latest Cadence height: %w", err)
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit final Cadence height update: %w", err)
	}

	t.log.Info().
		Uint64("new-latest-evm-height", targetEVMHeight).
		Uint64("new-latest-cadence-height", cadenceHeight).
		Msg("storage truncation completed")

	return nil
}

// truncateBatch deletes all data for blocks in the range [startHeight, endHeight].
// After deletion, it updates latestEVMHeight to startHeight - 1.
func (t *TruncateStorage) truncateBatch(startHeight, endHeight uint64) error {
	batch := t.store.NewBatch()
	defer func() {
		if err := batch.Close(); err != nil {
			t.log.Error().Err(err).Msg("failed to close batch")
		}
	}()

	// Process each height in the batch (backwards for safety)
	for height := endHeight; height >= startHeight; height-- {
		if err := t.deleteBlockData(height, batch); err != nil {
			// If block not found, it might have been already deleted or never existed
			if err != errs.ErrEntityNotFound {
				return fmt.Errorf("failed to delete block data at height %d: %w", height, err)
			}
			t.log.Debug().Uint64("height", height).Msg("block not found, skipping")
		}

		// Prevent underflow
		if height == 0 {
			break
		}
	}

	// Update latestEVMHeight to reflect new boundary
	newLatestHeight := startHeight - 1
	if err := t.store.set(latestEVMHeightKey, nil, uint64Bytes(newLatestHeight), batch); err != nil {
		return fmt.Errorf("failed to update latest EVM height to %d: %w", newLatestHeight, err)
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	t.log.Debug().
		Uint64("new-latest-height", newLatestHeight).
		Msg("batch committed")

	return nil
}

// deleteBlockData deletes all data associated with a single block height.
func (t *TruncateStorage) deleteBlockData(height uint64, batch *pebble.Batch) error {
	heightBytes := uint64Bytes(height)

	// Get block to retrieve block hash and transaction hashes
	block, err := t.blocks.GetByHeight(height)
	if err != nil {
		return err
	}

	blockHash, err := block.Hash()
	if err != nil {
		return fmt.Errorf("failed to get block hash: %w", err)
	}

	// Delete transactions and traces using transaction hashes from block
	for _, txHash := range block.TransactionHashes {
		// Delete transaction
		if err := t.store.delete(txIDKey, txHash.Bytes(), batch); err != nil {
			return fmt.Errorf("failed to delete transaction %s: %w", txHash.Hex(), err)
		}

		// Delete trace
		if err := t.store.delete(traceTxIDKey, txHash.Bytes(), batch); err != nil {
			return fmt.Errorf("failed to delete trace for tx %s: %w", txHash.Hex(), err)
		}

		// Delete receipt tx ID to height mapping
		if err := t.store.delete(receiptTxIDToHeightKey, txHash.Bytes(), batch); err != nil {
			return fmt.Errorf("failed to delete receipt tx mapping for %s: %w", txHash.Hex(), err)
		}
	}

	// Delete receipts and blooms by height
	if err := t.store.delete(receiptHeightKey, heightBytes, batch); err != nil {
		return fmt.Errorf("failed to delete receipts at height %d: %w", height, err)
	}

	if err := t.store.delete(bloomHeightKey, heightBytes, batch); err != nil {
		return fmt.Errorf("failed to delete blooms at height %d: %w", height, err)
	}

	// Delete block indexes
	if err := t.store.delete(blockHeightKey, heightBytes, batch); err != nil {
		return fmt.Errorf("failed to delete block at height %d: %w", height, err)
	}

	if err := t.store.delete(blockIDToHeightKey, blockHash.Bytes(), batch); err != nil {
		return fmt.Errorf("failed to delete block ID mapping for %s: %w", blockHash.Hex(), err)
	}

	if err := t.store.delete(evmHeightToCadenceHeightKey, heightBytes, batch); err != nil {
		return fmt.Errorf("failed to delete EVM to Cadence height mapping at %d: %w", height, err)
	}

	if err := t.store.delete(evmHeightToCadenceIDKey, heightBytes, batch); err != nil {
		return fmt.Errorf("failed to delete EVM to Cadence ID mapping at %d: %w", height, err)
	}

	return nil
}

// TruncateRegisters removes all register entries above the specified EVM height.
// This is separate from TruncateStorage because register storage uses a different
// key format with height encoded using one's complement.
//
// The method iterates over all register keys and deletes those with height > targetHeight.
// It processes in batches for efficiency.
func (r *RegisterStorage) TruncateAtHeight(targetHeight uint64) error {
	db := r.store.db

	// Create iterator over all register keys
	iter, err := db.NewIter(&pebble.IterOptions{
		LowerBound: []byte{registerKeyMarker},
		UpperBound: []byte{registerKeyMarker + 1},
	})
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() {
		if err := iter.Close(); err != nil {
			r.store.log.Error().Err(err).Msg("failed to close iterator")
		}
	}()

	batch := r.store.NewBatch()
	defer func() {
		if err := batch.Close(); err != nil {
			r.store.log.Error().Err(err).Msg("failed to close batch")
		}
	}()

	deletedCount := 0
	batchCount := 0

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()

		// Extract height from key (last 8 bytes, stored as one's complement)
		if len(key) < MinLookupKeyLen {
			continue
		}

		heightOnesComplement := binary.BigEndian.Uint64(key[len(key)-8:])
		height := ^heightOnesComplement // Convert back from one's complement

		if height > targetHeight {
			// Make a copy of the key since we're deleting while iterating
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)

			if err := batch.Delete(keyCopy, nil); err != nil {
				return fmt.Errorf("failed to delete register at height %d: %w", height, err)
			}
			deletedCount++
			batchCount++

			// Commit batch periodically
			if batchCount >= 10000 {
				if err := batch.Commit(pebble.Sync); err != nil {
					return fmt.Errorf("failed to commit register truncation batch: %w", err)
				}
				batch = r.store.NewBatch()
				batchCount = 0
			}
		}
	}

	// Commit remaining
	if batchCount > 0 {
		if err := batch.Commit(pebble.Sync); err != nil {
			return fmt.Errorf("failed to commit final register truncation batch: %w", err)
		}
	}

	return nil
}
