package requester

import (
	"fmt"
	"sync"
	"time"

	flowsdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	flowGo "github.com/onflow/flow-go/model/flow"
)

var ErrNoKeysAvailable = fmt.Errorf("no keys available")

const accountKeyExpiry = 10 * time.Second

type AccountKey struct {
	flowsdk.AccountKey

	mu       sync.Mutex
	ks       *KeyStore
	Address  flowsdk.Address
	Signer   crypto.Signer
	lastUsed time.Time
}

// Done unlocks a key after use and puts it back into the pool.
func (k *AccountKey) Done() {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.ks.availableKeys <- k
}

func (k *AccountKey) SetProposerPayerAndSign(
	tx *flowsdk.Transaction,
	account *flowsdk.Account,
) error {
	if k.Address != account.Address {
		return fmt.Errorf(
			"expected address: %v, got address: %v",
			k.Address,
			account.Address,
		)
	}
	if k.Index >= uint32(len(account.Keys)) {
		return fmt.Errorf(
			"key index: %d exceeds keys length: %d",
			k.Index,
			len(account.Keys),
		)
	}
	seqNumber := account.Keys[k.Index].SequenceNumber

	return tx.
		SetProposalKey(k.Address, k.Index, seqNumber).
		SetPayer(k.Address).
		SignEnvelope(k.Address, k.Index, k.Signer)
}

func (k *AccountKey) expired() bool {
	return time.Since(k.lastUsed) > flowGo.DefaultTransactionExpiry
}

type KeyLock interface {
	LockKey(txID flowsdk.Identifier, key *AccountKey)
	UnlockKey(txID flowsdk.Identifier)
}

type KeyStore struct {
	availableKeys chan *AccountKey
	usedKeys      map[flowsdk.Identifier]*AccountKey
	size          int
}

var _ KeyLock = (*KeyStore)(nil)

func NewKeyStore(keys []*AccountKey) *KeyStore {
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

	go ks.keyExpiryChecker()

	return ks
}

func (k *KeyStore) AvailableKeys() int {
	return k.size - len(k.usedKeys)
}

func (k *KeyStore) Take() (*AccountKey, error) {
	select {
	case key := <-k.availableKeys:
		return key, nil
	default:
		return nil, ErrNoKeysAvailable
	}
}

func (k *KeyStore) LockKey(txID flowsdk.Identifier, key *AccountKey) {
	key.mu.Lock()
	defer key.mu.Unlock()

	key.lastUsed = time.Now()
	k.usedKeys[txID] = key
}

func (k *KeyStore) UnlockKey(txID flowsdk.Identifier) {
	key, ok := k.usedKeys[txID]
	if ok && key != nil {
		key.Done()
		delete(k.usedKeys, txID)
	}
}

func (k *KeyStore) keyExpiryChecker() {
	for range time.Tick(accountKeyExpiry) {
		for txID, key := range k.usedKeys {
			if key.expired() {
				k.UnlockKey(txID)
			}
		}
	}
}
