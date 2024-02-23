package requester

import (
	"fmt"
	"github.com/onflow/flow-go-sdk/crypto"
	"sync/atomic"
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
	keys   []crypto.PrivateKey
	hasher crypto.Hasher
	index  *atomic.Int32
	keyLen int32
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

	// init the key index to 0 value
	i := &atomic.Int32{}
	i.Store(0)

	return &KeyRotationSigner{
		keys:   keys,
		hasher: hasher,
		index:  i,
		keyLen: int32(len(keys)),
	}, nil
}

func (k *KeyRotationSigner) Sign(message []byte) ([]byte, error) {
	return k.nextKey().Sign(message, k.hasher)
}

func (k *KeyRotationSigner) PublicKey() crypto.PublicKey {
	current := k.index.Load()
	return k.keys[current].PublicKey()
}

func (k *KeyRotationSigner) nextKey() crypto.PrivateKey {
	for {
		current := k.index.Load()
		// this makes sure the index is always rotating within the bounds of the keys slice
		next := (current + 1) % k.keyLen

		if k.index.CompareAndSwap(current, next) {
			return k.keys[next]
		}
	}
}
