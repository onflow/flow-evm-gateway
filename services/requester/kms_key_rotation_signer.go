package requester

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/rs/zerolog"
)

var _ crypto.Signer = &KMSKeyRotationSigner{}

// KMSKeyRotationSigner is a crypto signer that contains a pool of
// `crypto.Signer`[1] objects, each of which is tied to a Cloud KMS
// asymmetric signing key.
// It keeps track of the signer/key combination that should be used for
// the next incoming signing request. This allows for faster submission
// of transactions to the network, due to a sequence number not being
// reused between different keys used.
// It also contains logic to queue up signature requests and in case
// there are more transactions pending than the keys in the pool it
// will wait for transactions to get executed so the new sequence key
// can be obtained.
// The signer is concurrency-safe.
//
// [1](https://github.com/onflow/flow-go-sdk/blob/master/crypto/cloudkms/signer.go#L37)
type KMSKeyRotationSigner struct {
	mux        sync.RWMutex
	kmsSigners []*cloudkms.Signer
	index      int
	signersLen int
	logger     zerolog.Logger
}

// NewKMSKeyRotationSigner returns a new KMSKeyRotationSigner,
// for the given slice of Cloud KMS keys.
func NewKMSKeyRotationSigner(
	ctx context.Context,
	keys []cloudkms.Key,
	logger zerolog.Logger,
) (*KMSKeyRotationSigner, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf(
			"could not create KMS key rotation signer, no KMS keys provided",
		)
	}

	kmsClient, err := cloudkms.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to create Cloud KMS client: %w", err)
	}

	kmsSigners := make([]*cloudkms.Signer, len(keys))
	for i, key := range keys {
		kmsSigner, err := kmsClient.SignerForKey(ctx, key)
		if err != nil {
			return nil, fmt.Errorf(
				"could not create KMS signer for the key with ID: %s: %w",
				key.KeyID,
				err,
			)
		}
		kmsSigners[i] = kmsSigner
	}

	logger = logger.With().Str("component", "cloud_kms_signer").Logger()

	return &KMSKeyRotationSigner{
		kmsSigners: kmsSigners,
		signersLen: len(kmsSigners),
		logger:     logger,
	}, nil
}

// Sign signs the message and then rotates to the next key.
// Note: if you want to get the public key pair, you should first call
// PublicKey and then Sign.
func (s *KMSKeyRotationSigner) Sign(message []byte) ([]byte, error) {
	defer func(start time.Time) {
		elapsed := time.Since(start)
		s.logger.Debug().
			Int64("duration", elapsed.Milliseconds()).
			Msg("messaged was signed")
	}(time.Now())

	s.mux.Lock()
	defer s.mux.Unlock()

	// Get the next available signer, for the message signing request.
	signer := s.kmsSigners[s.index]
	// Update the available signer that should be used for the next
	// message signing request.
	s.index = (s.index + 1) % s.signersLen

	signature, err := signer.Sign(message)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to sign message with public key %s: %w",
			signer.PublicKey(),
			err,
		)
	}

	return signature, err
}

// PublicKey returns the current public key which is available for signing.
func (s *KMSKeyRotationSigner) PublicKey() crypto.PublicKey {
	s.mux.RLock()
	defer s.mux.RUnlock()

	// TODO(m-Peter): Make this function to wait if the transaction that used this
	// public key is not yet sealed on the network, this is so it prevents using
	// the same public key for fetching key sequence number before the transaction
	// that already used it is not executed and thus the key would be incremented.
	return s.kmsSigners[s.index].PublicKey()
}
