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

var _ crypto.Signer = &CloudKMSKeyRotationSigner{}

// CloudKMSKeyRotationSigner is a crypto signer that contains a pool of
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
type CloudKMSKeyRotationSigner struct {
	mux        sync.RWMutex
	kmsSigners []*cloudkms.Signer
	index      int
	signersLen int
	logger     zerolog.Logger
}

// NewSignerForKeys returns a new CloudKMSKeyRotationSigner, for the
// given slice of Cloud KMS keys.
func NewSignerForKeys(
	ctx context.Context,
	client *cloudkms.Client,
	keys []cloudkms.Key,
	logger zerolog.Logger,
) (*CloudKMSKeyRotationSigner, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("no asymmetric signing keys provided")
	}

	kmsSigners := make([]*cloudkms.Signer, len(keys))
	for i, key := range keys {
		kmsSigner, err := client.SignerForKey(ctx, key)
		if err != nil {
			return nil, fmt.Errorf(
				"could not create signer for key with ID: %s: %w",
				key.KeyID,
				err,
			)
		}
		kmsSigners[i] = kmsSigner
	}

	logger = logger.With().Str("component", "cloud_kms_signer").Logger()

	return &CloudKMSKeyRotationSigner{
		kmsSigners: kmsSigners,
		signersLen: len(kmsSigners),
		logger:     logger,
	}, nil
}

// Sign signs the message and then rotates to the next key.
// Note: if you want to get the public key pair, you should first call
// PublicKey and then Sign.
func (s *CloudKMSKeyRotationSigner) Sign(message []byte) ([]byte, error) {
	defer s.timeTrack(time.Now())

	s.mux.Lock()
	defer s.mux.Unlock()

	signer := s.kmsSigners[s.index]
	s.index = (s.index + 1) % s.signersLen

	return signer.Sign(message)
}

// PublicKey returns the current public key which is available for signing.
func (s *CloudKMSKeyRotationSigner) PublicKey() crypto.PublicKey {
	s.mux.RLock()
	defer s.mux.RUnlock()

	// TODO(m-Peter): Make this function to wait if the transaction that used this
	// public key is not yet sealed on the network, this is so it prevents using
	// the same public key for fetching key sequence number before the transaction
	// that already used it is not executed and thus the key would be incremented.
	return s.kmsSigners[s.index].PublicKey()
}

func (s *CloudKMSKeyRotationSigner) timeTrack(start time.Time) {
	elapsed := time.Since(start)
	s.logger.Info().
		Int64("duration", elapsed.Milliseconds()).
		Msg("messaged was signed")
}
