package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
	"math/big"
	"sync"
)

var _ storage.AccountIndexer = &Accounts{}

type Accounts struct {
	store *Storage
	mux   sync.RWMutex
	// todo LRU caching with size limit
	nonceCache map[common.Address][]uint64
}

func NewAccounts(db *Storage) *Accounts {
	return &Accounts{
		store:      db,
		mux:        sync.RWMutex{},
		nonceCache: make(map[common.Address][]uint64),
	}
}

func (a *Accounts) Update(tx *gethTypes.Transaction, receipt *gethTypes.Receipt) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	from, err := gethTypes.Sender(gethTypes.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return err
	}

	nonce, height, err := a.getNonce(from)
	if err != nil {
		return err
	}

	// update nonce only if the transaction is new, this makes update idempotent,
	// so we can update multiple times with same receipt but nonce won't get increased
	if receipt.BlockNumber.Uint64() > height {
		nonce += 1
	} else {
		return nil
	}

	data := encodeNonce(nonce, receipt.BlockNumber.Uint64())
	err = a.store.set(accountNonceKey, from.Bytes(), data)
	if err != nil {
		return err
	}

	a.nonceCache[from] = []uint64{nonce, height}
	return nil
}

func (a *Accounts) getNonce(address common.Address) (uint64, uint64, error) {
	data, ok := a.nonceCache[address]
	if ok { // if present in cache return it
		return data[0], data[1], nil
	}

	val, err := a.store.get(accountNonceKey, address.Bytes())
	if err != nil {
		// if no nonce was yet saved for the account the nonce is 0
		if errors.Is(err, errs.ErrNotFound) {
			return 0, 0, nil
		}

		return 0, 0, err
	}

	nonce, height, err := decodeNonce(val)
	if err != nil {
		return 0, 0, err
	}

	a.nonceCache[address] = []uint64{nonce, height}
	return nonce, height, nil
}

func (a *Accounts) GetNonce(address *common.Address) (uint64, error) {
	a.mux.RLock()
	defer a.mux.RUnlock()
	nonce, _, err := a.getNonce(*address)
	if err != nil {
		return 0, fmt.Errorf("failed to get nonce: %w", err)
	}

	return nonce, nil
}

func (a *Accounts) GetBalance(address *common.Address) (*big.Int, error) {
	panic("not supported")
}

// decodeNonce converts nonce data into nonce and height
func decodeNonce(data []byte) (uint64, uint64, error) {
	if len(data) != 16 {
		return 0, 0, fmt.Errorf("invalid nonce data")
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
