package pebble

import (
	"encoding/binary"
	"errors"
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
	nonceCache map[*common.Address]uint64
}

func (a *Accounts) Update(tx *gethTypes.Transaction) error {
	a.mux.Lock()
	defer a.mux.Unlock()

	from, err := gethTypes.Sender(gethTypes.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		panic(err)
	}

	nonce, err := a.getNonce(&from)
	if err != nil {
		return err
	}

	// update nonce
	nonce += 1

	err = a.store.set(accountNonceKey, from.Bytes(), uint64Bytes(nonce))
	if err != nil {
		return err
	}

	a.nonceCache[&from] = nonce
	return nil
}

func (a *Accounts) getNonce(address *common.Address) (uint64, error) {
	nonce, ok := a.nonceCache[address]
	if ok { // if present in cache return it
		return nonce, nil
	}

	val, err := a.store.get(accountNonceKey, address.Bytes())
	if err != nil {
		// if no nonce was yet saved for the account the nonce is 0
		if errors.Is(err, errs.NotFound) {
			return 0, nil
		}

		return 0, err
	}

	nonce = binary.BigEndian.Uint64(val)
	a.nonceCache[address] = nonce
	return nonce, nil
}

func (a *Accounts) GetNonce(address *common.Address) (uint64, error) {
	a.mux.RLock()
	defer a.mux.RUnlock()
	return a.getNonce(address)
}

func (a *Accounts) GetBalance(address *common.Address) (*big.Int, error) {
	panic("not supported")
}
