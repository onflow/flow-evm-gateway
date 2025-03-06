package requester

import (
	"fmt"
	"sync"

	flowsdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/model/flow"
)

var ErrNoKeysAvailable = fmt.Errorf("no signing keys available")

const accountKeyBlockExpiration = flow.DefaultTransactionExpiry

type AccountKey struct {
	flowsdk.AccountKey

	mu            sync.Mutex
	ks            *KeyStore
	Address       flowsdk.Address
	Signer        crypto.Signer
	lastUsedBlock uint64
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

type KeyLock interface {
	LockKey(
		txID flowsdk.Identifier,
		referenceBlockHeight uint64,
		key *AccountKey,
	)
	UnlockKey(txID flowsdk.Identifier)
	Notify(blockHeight uint64)
}

type KeyStore struct {
	availableKeys chan *AccountKey
	usedKeys      map[flowsdk.Identifier]*AccountKey
	size          int

	keyMu sync.RWMutex
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

	return ks
}

// AvailableKeys returns the number of keys available for use.
func (k *KeyStore) AvailableKeys() int {
	k.keyMu.RLock()
	defer k.keyMu.RUnlock()

	return k.size - len(k.usedKeys)
}

// Take reserves a key for use in a transaction.
func (k *KeyStore) Take() (*AccountKey, error) {
	select {
	case key := <-k.availableKeys:
		return key, nil
	default:
		return nil, ErrNoKeysAvailable
	}
}

// LockKey locks a key for use in a transaction.
func (k *KeyStore) LockKey(
	txID flowsdk.Identifier,
	referenceBlockHeight uint64,
	key *AccountKey,
) {
	key.mu.Lock()
	key.lastUsedBlock = referenceBlockHeight
	key.mu.Unlock()

	k.keyMu.Lock()
	k.usedKeys[txID] = key
	k.keyMu.Unlock()
}

// UnlockKey unlocks a key after use and puts it back into the pool.
func (k *KeyStore) UnlockKey(txID flowsdk.Identifier) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()

	k.unlockKey(txID)
}

func (k *KeyStore) unlockKey(txID flowsdk.Identifier) {
	key, ok := k.usedKeys[txID]
	if ok && key != nil {
		key.Done()
		delete(k.usedKeys, txID)
	}
}

// Notify is called to notify the KeyStore of a new ingested block height.
// This is used to expire locks on keys after the transaction expiry time.
func (k *KeyStore) Notify(blockHeight uint64) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()

	for txID, key := range k.usedKeys {
		if blockHeight-key.lastUsedBlock >= accountKeyBlockExpiration {
			k.unlockKey(txID)
		}
	}
}
