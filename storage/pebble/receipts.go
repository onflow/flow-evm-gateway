package pebble

import (
	"encoding/binary"
	"fmt"
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"math/big"
	"sync"
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

func (r *Receipts) Store(receipt *gethTypes.Receipt) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	// convert to storage receipt to preserve all values
	rr := (*models.StorageReceipt)(receipt)
	val, err := rlp.EncodeToBytes(rr)
	if err != nil {
		return err
	}

	batch := r.store.newBatch()
	defer batch.Close()

	height := receipt.BlockNumber.Bytes()
	if err := r.store.set(receiptTxIDToHeightKey, receipt.TxHash.Bytes(), height, batch); err != nil {
		return fmt.Errorf("failed to store receipt tx height: %w", err)
	}

	// todo if there are more transactions per block we need to update this
	if err := r.store.set(receiptHeightKey, height, val, batch); err != nil {
		return fmt.Errorf("failed to store receipt height: %w", err)
	}

	if err := r.store.set(bloomHeightKey, height, receipt.Bloom.Bytes(), batch); err != nil {
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

	rcp, err := r.getByBlockHeight(height)
	if err != nil {
		return nil, fmt.Errorf("failed to get receipt by height: %w", err)
	}

	return rcp, nil
}

func (r *Receipts) GetByBlockHeight(height *big.Int) (*gethTypes.Receipt, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()
	return r.getByBlockHeight(height.Bytes())
}

func (r *Receipts) getByBlockHeight(height []byte) (*gethTypes.Receipt, error) {
	val, err := r.store.get(receiptHeightKey, height)
	if err != nil {
		return nil, err
	}

	var rcp models.StorageReceipt
	err = rlp.DecodeBytes(val, &rcp)
	if err != nil {
		return nil, err
	}

	// dynamically populate the values since they are not stored to save space
	for i, l := range rcp.Logs {
		l.BlockHash = rcp.BlockHash
		l.BlockNumber = rcp.BlockNumber.Uint64()
		l.TxHash = rcp.TxHash
		l.TxIndex = rcp.TransactionIndex
		l.Index = uint(i)
	}

	return (*gethTypes.Receipt)(&rcp), nil
}

func (r *Receipts) BloomsForBlockRange(start, end *big.Int) ([]*gethTypes.Bloom, []*big.Int, error) {
	if start.Cmp(end) > 0 {
		return nil, nil, fmt.Errorf("start is bigger than end: %w", errors.ErrInvalidRange)
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
			errors.ErrInvalidRange,
		)
	}

	if end.Uint64() > last {
		return nil, nil, fmt.Errorf(
			"end value %d is not within the indexed range of [0 - %d]: %w",
			end,
			last,
			errors.ErrInvalidRange,
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

		bloom := gethTypes.BytesToBloom(val)
		h := stripPrefix(iterator.Key())
		height := new(big.Int).SetBytes(h)

		blooms = append(blooms, &bloom)
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
