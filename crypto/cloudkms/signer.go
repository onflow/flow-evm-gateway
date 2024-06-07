package cloudkms

import (
	"context"
	"fmt"
	"sync"

	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go-sdk/crypto/cloudkms"
)

var _ crypto.Signer = &KeyRotationSigner{}

type KeyRotationSigner struct {
	mux        sync.RWMutex
	kmsSigners []*cloudkms.Signer
	index      int
	signersLen int
}

func NewSignerForKeys(
	ctx context.Context,
	client *cloudkms.Client,
	keys []cloudkms.Key,
) (*KeyRotationSigner, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("no asymmetric signing keys provided")
	}

	kmsSigners := make([]*cloudkms.Signer, len(keys))
	for i, key := range keys {
		kmsSigner, err := client.SignerForKey(ctx, key)
		if err != nil {
			return nil, fmt.Errorf(
				"could not create signer for key with ID: %s",
				key.KeyID,
			)
		}
		kmsSigners[i] = kmsSigner
	}

	return &KeyRotationSigner{
		kmsSigners: kmsSigners,
		signersLen: len(kmsSigners),
	}, nil
}

func (s *KeyRotationSigner) Sign(message []byte) ([]byte, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	signer := s.kmsSigners[s.index]
	s.index = (s.index + 1) % s.signersLen

	return signer.Sign(message)
}

func (s *KeyRotationSigner) PublicKey() crypto.PublicKey {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return s.kmsSigners[s.index].PublicKey()
}
