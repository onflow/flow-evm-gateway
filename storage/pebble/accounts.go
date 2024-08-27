package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.AccountIndexer = &Accounts{}

type Accounts struct {
	store *Storage
	mux   sync.RWMutex
}

func NewAccounts(db *Storage) *Accounts {
	return &Accounts{
		store: db,
		mux:   sync.RWMutex{},
	}
}

func (a *Accounts) Update(
	tx models.Transaction,
	receipt *models.Receipt,
	batch *pebble.Batch,
) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	from, err := tx.From()
	if err != nil {
		return err
	}

	nonce, height, err := a.getNonce(from, batch)
	if err != nil {
		return err
	}

	// make sure the transaction height is bigger than the height we already
	// recorded for the nonce. this makes the operation idempotent and safer.
	txHeight := receipt.BlockNumber.Uint64()
	if txHeight <= height {
		return nil
	}

	nonce += 1

	data := encodeNonce(nonce, txHeight)
	return a.store.set(accountNonceKey, from.Bytes(), data, batch)
}

func (a *Accounts) getNonce(address common.Address, batch *pebble.Batch) (uint64, uint64, error) {
	var val []byte
	var err error
	if batch != nil {
		val, err = a.store.batchGet(batch, accountNonceKey, address.Bytes())
	} else {
		val, err = a.store.get(accountNonceKey, address.Bytes())
	}
	if err != nil {
		// if no nonce was yet saved for the account the nonce is 0
		if errors.Is(err, errs.ErrEntityNotFound) {
			return 0, 0, nil
		}

		return 0, 0, err
	}

	nonce, height, err := decodeNonce(val)
	if err != nil {
		return 0, 0, err
	}

	return nonce, height, nil
}

func (a *Accounts) GetNonce(address common.Address) (uint64, error) {
	a.mux.RLock()
	defer a.mux.RUnlock()
	nonce, _, err := a.getNonce(address, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get nonce of address: %s, with: %w", address, err)
	}

	return nonce, nil
}

func (a *Accounts) GetBalance(address common.Address) (*big.Int, error) {
	panic("not supported")
}

// decodeNonce converts nonce data into nonce and height
func decodeNonce(data []byte) (uint64, uint64, error) {
	if len(data) != 16 {
		return 0, 0, fmt.Errorf("invalid nonce data, expected length: %d, got: %d", 16, len(data))
	}
	nonce := binary.BigEndian.Uint64(data[:8])
	height := binary.BigEndian.Uint64(data[8:])

	return nonce, height, nil
}

// encodeNonce converts nonce and height into nonce data
func encodeNonce(nonce uint64, height uint64) []byte {
	payload := make([]byte, 16)
	for i, b := range uint64Bytes(nonce) {
		payload[i] = b
	}
	for i, b := range uint64Bytes(height) {
		payload[i+8] = b
	}

	return payload
}
