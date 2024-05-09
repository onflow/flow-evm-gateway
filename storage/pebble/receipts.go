package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/rlp"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
)

var _ storage.ReceiptIndexer = &Receipts{}

type Receipts struct {
	store *Storage
	mux   sync.RWMutex
}

func NewReceipts(store *Storage) *Receipts {
	return &Receipts{
		store: store,
		mux:   sync.RWMutex{},
	}
}

// Store receipt in the index.
//
// Storing receipt will create multiple indexes, each receipt has a transaction ID,
// and a block height. We create following mappings:
// - receipt transaction ID => block height bytes
// - receipt block height => list of encoded receipts (1+ per block)
// - receipt block height => list of bloom filters (1+ per block)
func (r *Receipts) Store(receipt *gethTypes.Receipt) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	// we must first retrieve any already saved receipts at the provided height,
	// and if found we must add to the list, because this method can be called multiple
	// times when indexing a single EVM height, which can contain multiple receipts
	blockHeight := receipt.BlockNumber.Bytes()
	receipts, err := r.getByBlockHeight(blockHeight)
	if err != nil && !errors.Is(err, errs.ErrNotFound) { // anything but not found is failure
		return fmt.Errorf("failed to store receipt to height, retrieve exisint receipt errror: %w", err)
	}

	// same goes for blooms
	blooms, err := r.getBloomsByBlockHeight(blockHeight)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return fmt.Errorf("failed to store receipt to height, retrieve existing blooms error: %w", err)
	}

	// add new receipt to the list of all receipts, if the receipts do not yet exist at the
	// provided height, the above get will return ErrNotFound (which we ignore) and the bellow
	// line will init an empty receipts slice with only the provided receipt
	receipts = append(receipts, receipt)
	blooms = append(blooms, &receipt.Bloom)

	batch := r.store.newBatch()
	defer batch.Close()

	// convert to storage receipt to preserve all values
	storeReceipts := make([]*models.StorageReceipt, len(receipts))
	for i, rr := range receipts {
		storeReceipts[i] = (*models.StorageReceipt)(rr)
	}

	val, err := rlp.EncodeToBytes(storeReceipts)
	if err != nil {
		return err
	}

	height := receipt.BlockNumber.Bytes()

	if err := r.store.set(receiptTxIDToHeightKey, receipt.TxHash.Bytes(), height, batch); err != nil {
		return fmt.Errorf("failed to store receipt tx height: %w", err)
	}

	if err := r.store.set(receiptHeightKey, height, val, batch); err != nil {
		return fmt.Errorf("failed to store receipt height: %w", err)
	}

	bloomVal, err := rlp.EncodeToBytes(blooms)
	if err != nil {
		return fmt.Errorf("failed to encode blooms: %w", err)
	}

	if err := r.store.set(bloomHeightKey, height, bloomVal, batch); err != nil {
		return fmt.Errorf("failed to store bloom height: %w", err)
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit receipt batch: %w", err)
	}

	return nil
}

func (r *Receipts) GetByTransactionID(ID common.Hash) (*gethTypes.Receipt, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	height, err := r.store.get(receiptTxIDToHeightKey, ID.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to get receipt by tx ID: %w", err)
	}

	receipts, err := r.getByBlockHeight(height)
	if err != nil {
		return nil, fmt.Errorf("failed to get receipt by height: %w", err)
	}

	for _, rcp := range receipts {
		if rcp.TxHash.Cmp(ID) == 0 {
			return rcp, nil
		}
	}

	return nil, errs.ErrNotFound
}

func (r *Receipts) GetByBlockHeight(height *big.Int) ([]*gethTypes.Receipt, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()
	return r.getByBlockHeight(height.Bytes())
}

func (r *Receipts) getByBlockHeight(height []byte) ([]*gethTypes.Receipt, error) {
	val, err := r.store.get(receiptHeightKey, height)
	if err != nil {
		return nil, err
	}

	var storeReceipts []*models.StorageReceipt
	if err = rlp.DecodeBytes(val, &storeReceipts); err != nil {
		// todo remove this after previewnet is reset
		// try to decode single receipt (breaking change migration)
		var storeReceipt models.StorageReceipt
		if err = rlp.DecodeBytes(val, &storeReceipt); err != nil {
			return nil, err
		}

		storeReceipts = []*models.StorageReceipt{&storeReceipt}
	}

	receipts := make([]*gethTypes.Receipt, len(storeReceipts))
	for i, rcp := range storeReceipts {
		// dynamically populate the values since they are not stored to save space
		for i, l := range rcp.Logs {
			l.BlockHash = rcp.BlockHash
			l.BlockNumber = rcp.BlockNumber.Uint64()
			l.TxHash = rcp.TxHash
			l.TxIndex = rcp.TransactionIndex
			l.Index = uint(i)
		}

		receipts[i] = (*gethTypes.Receipt)(rcp)
	}

	return receipts, nil
}

func (r *Receipts) getBloomsByBlockHeight(height []byte) ([]*gethTypes.Bloom, error) {
	val, err := r.store.get(bloomHeightKey, height)
	if err != nil {
		return nil, fmt.Errorf("failed to get bloom at height: %w", err)
	}

	var blooms []*gethTypes.Bloom
	if err := rlp.DecodeBytes(val, &blooms); err != nil {
		return nil, fmt.Errorf("failed to decode blooms at height: %w", err)
	}

	return blooms, nil
}

func (r *Receipts) BloomsForBlockRange(start, end *big.Int) ([]*gethTypes.Bloom, []*big.Int, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	if start.Cmp(end) > 0 {
		return nil, nil, fmt.Errorf("start is bigger than end: %w", errs.ErrInvalidRange)
	}

	// make sure the first and last height are within indexed values
	last, err := r.getLast()
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting first and last height: %w", err)
	}

	if start.Uint64() > last {
		return nil, nil, fmt.Errorf(
			"start value %d is not within the indexed range of [0 - %d]: %w",
			start,
			last,
			errs.ErrInvalidRange,
		)
	}

	if end.Uint64() > last {
		return nil, nil, fmt.Errorf(
			"end value %d is not within the indexed range of [0 - %d]: %w",
			end,
			last,
			errs.ErrInvalidRange,
		)
	}

	// we increase end by 1 since the range is exclusive at the upper boundary: [start, end)
	endInclusive := new(big.Int).Add(end, big.NewInt(1))
	iterator, err := r.store.db.NewIter(&pebble.IterOptions{
		LowerBound: makePrefix(bloomHeightKey, start.Bytes()),        // inclusive
		UpperBound: makePrefix(bloomHeightKey, endInclusive.Bytes()), // exclusive
	})
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		err := iterator.Close()
		if err != nil {
			r.store.log.Error().Err(err).Msg("failed to close receipt iterator")
		}
	}()

	caps := end.Div(end, start).Uint64() // max capacity for slices
	blooms := make([]*gethTypes.Bloom, 0, caps)
	heights := make([]*big.Int, 0, caps)

	for iterator.First(); iterator.Valid(); iterator.Next() {
		val, err := iterator.ValueAndErr()
		if err != nil {
			return nil, nil, err
		}

		var bloomsHeight []*gethTypes.Bloom
		if err := rlp.DecodeBytes(val, &bloomsHeight); err != nil {
			return nil, nil, fmt.Errorf("failed to decode blooms: %w", err)
		}

		h := stripPrefix(iterator.Key())
		height := new(big.Int).SetBytes(h)

		blooms = append(blooms, bloomsHeight...)
		heights = append(heights, height)
	}

	return blooms, heights, nil
}

func (r *Receipts) getLast() (uint64, error) {
	l, err := r.store.get(latestEVMHeightKey)
	if err != nil {
		return 0, fmt.Errorf("failed getting latest height: %w", err)
	}

	return binary.BigEndian.Uint64(l), nil
}
