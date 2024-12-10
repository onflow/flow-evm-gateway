package requester

import (
	"fmt"
	"sync"

	flowsdk "github.com/onflow/flow-go-sdk"

	"github.com/onflow/flow-go-sdk/crypto"
)

var ErrNoKeysAvailable = fmt.Errorf("no keys available")

type AccountKey struct {
	flowsdk.AccountKey

	mu      sync.Mutex
	ks      *Keystore
	Address flowsdk.Address
	Signer  crypto.Signer
	inuse   bool
}

// Done unlocks a key after use and puts it back into the pool.
func (k *AccountKey) Done() {
	k.markUnused()
	k.ks.availableKeys <- k
}

// IncrementSequenceNumber is called when a key was successfully used to
// sign a transaction as the proposer. It increments the sequence number.
func (k *AccountKey) IncrementSequenceNumber() error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.inuse {
		return fmt.Errorf("key with index %d not locked", k.Index)
	}

	k.SequenceNumber++
	return nil
}

func (k *AccountKey) SetProposerPayerAndSign(tx *flowsdk.Transaction) error {
	return tx.
		SetProposalKey(k.Address, k.Index, k.SequenceNumber).
		SetPayer(k.Address).
		SignEnvelope(k.Address, k.Index, k.Signer)
}

func (k *AccountKey) markUnused() {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.inuse = false
}

type Keystore struct {
	availableKeys chan *AccountKey
	usedKeys      map[flowsdk.Identifier]*AccountKey
	size          int
}

func NewKeystore(keys []*AccountKey) *Keystore {
	ks := &Keystore{
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

func (k *Keystore) Size() int {
	return k.size
}

func (k *Keystore) GetKey() (*AccountKey, error) {
	select {
	case key := <-k.availableKeys:
		key.mu.Lock()
		defer key.mu.Unlock()

		if key.inuse {
			return nil, fmt.Errorf("key with index %d already in use", key.Index)
		}
		key.inuse = true

		return key, nil
	default:
		return nil, ErrNoKeysAvailable
	}
}

func (k *Keystore) LockKey(txID flowsdk.Identifier, key *AccountKey) {
	k.usedKeys[txID] = key
}

func (k *Keystore) UnlockKey(txID flowsdk.Identifier) {
	key, ok := k.usedKeys[txID]
	if ok && key != nil {
		key.Done()
		delete(k.usedKeys, txID)
	}
}
