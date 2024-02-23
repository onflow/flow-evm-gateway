package requester

import (
	"fmt"
	"github.com/onflow/flow-go-sdk/crypto"
	"sync"
)

var _ crypto.Signer = &KeyRotationSigner{}

// KeyRotationSigner is a crypto signer that contains a pool of key pairs to sign with,
// and it rotates the key used for each signing request. This allows for faster
// submission of transactions to the network, due to a sequence number not being reused
// between different keys used.
// It also contains logic to queue up signature requests and in case there are more
// transactions pending than the keys in the pool it will wait for transactions to
// get executed so the new sequence key can be obtained.
// The signer is concurrency-safe.
type KeyRotationSigner struct {
	mux    sync.RWMutex
	keys   []crypto.PrivateKey
	hasher crypto.Hasher
	index  int
	keyLen int
}

func NewKeyRotationSigner(keys []crypto.PrivateKey, hashAlgo crypto.HashAlgorithm) (*KeyRotationSigner, error) {
	// check compatibility to form a signing key
	for _, pk := range keys {
		if !crypto.CompatibleAlgorithms(pk.Algorithm(), hashAlgo) {
			return nil, fmt.Errorf("signature algorithm %s and hashing algorithm are incompatible %s",
				pk.Algorithm(), hashAlgo)
		}
	}

	hasher, err := crypto.NewHasher(hashAlgo)
	if err != nil {
		return nil, fmt.Errorf("signer with hasher %s can't be instantiated with this function", hashAlgo)
	}

	return &KeyRotationSigner{
		keys:   keys,
		hasher: hasher,
		keyLen: len(keys),
	}, nil
}

// Sign signs the message and then rotates to the next key.
// note: if you want to get the public key pair, you should first call
// PublicKey and then Sign.
func (k *KeyRotationSigner) Sign(message []byte) ([]byte, error) {
	k.mux.Lock()
	defer k.mux.Unlock()

	sig, err := k.keys[k.index].Sign(message, k.hasher)
	k.index = (k.index + 1) % k.keyLen
	return sig, err
}

// PublicKey returns the current public key.
func (k *KeyRotationSigner) PublicKey() crypto.PublicKey {
	k.mux.RLock()
	defer k.mux.RUnlock()
	return k.keys[k.index].PublicKey()
}
