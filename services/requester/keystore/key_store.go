package keystore

import (
	"fmt"
	"sync"

	flowsdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/model/flow"
)

var ErrNoKeysAvailable = fmt.Errorf("no signing keys available")

const accountKeyBlockExpiration = flow.DefaultTransactionExpiry

type KeyLock interface {
	NotifyTransaction(txID flowsdk.Identifier)
	NotifyBlock(blockHeight uint64)
}

type KeyStore struct {
	availableKeys chan *AccountKey
	usedKeys      map[flowsdk.Identifier]*AccountKey
	size          int
	keyMu         sync.RWMutex
}

var _ KeyLock = (*KeyStore)(nil)

func New(keys []*AccountKey) *KeyStore {
	ks := &KeyStore{
		usedKeys: map[flowsdk.Identifier]*AccountKey{},
	}

	availableKeys := make(chan *AccountKey, len(keys))
	for _, key := range keys {
		key.ks = ks
		availableKeys <- key
	}
	ks.size = len(keys)
	ks.availableKeys = availableKeys

	return ks
}

// AvailableKeys returns the number of keys available for use.
func (k *KeyStore) AvailableKeys() int {
	return len(k.availableKeys)
}

// Take reserves a key for use in a transaction.
func (k *KeyStore) Take() (*AccountKey, error) {
	select {
	case key := <-k.availableKeys:
		if !key.lock() {
			// this should never happen and means there's a bug
			panic(fmt.Sprintf("key %d available, but locked", key.Index))
		}
		return key, nil
	default:
		return nil, ErrNoKeysAvailable
	}
}

// NotifyTransaction unlocks a key after use and puts it back into the pool.
func (k *KeyStore) NotifyTransaction(txID flowsdk.Identifier) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()

	k.unlockKey(txID)
}

// NotifyBlock is called to notify the KeyStore of a new ingested block height.
// Pending transactions older than a threshold number of blocks are removed.
func (k *KeyStore) NotifyBlock(blockHeight uint64) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()

	for txID, key := range k.usedKeys {
		if blockHeight-key.lastLockedBlock.Load() >= accountKeyBlockExpiration {
			k.unlockKey(txID)
		}
	}
}

func (k *KeyStore) unlockKey(txID flowsdk.Identifier) {
	if key, ok := k.usedKeys[txID]; ok {
		key.Done()
		delete(k.usedKeys, txID)
	}
}

func (k *KeyStore) release(key *AccountKey) {
	k.availableKeys <- key
}

func (k *KeyStore) setLockMetadata(
	key *AccountKey,
	txID flowsdk.Identifier,
) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()
	k.usedKeys[txID] = key
}
