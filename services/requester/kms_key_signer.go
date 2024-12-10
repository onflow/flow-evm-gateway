package requester

import (
	"context"
	"fmt"
	"time"

	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/rs/zerolog"
)

var _ crypto.Signer = &KMSKeySigner{}

// KMSKeySigner is a crypto signer that contains a `crypto.Signer`[1] object,
// which is tied to a Cloud KMS asymmetric signing key.
//
// [1](https://github.com/onflow/flow-go-sdk/blob/master/crypto/cloudkms/signer.go#L37)
type KMSKeySigner struct {
	kmsSigner *cloudkms.Signer
	logger    zerolog.Logger
}

// NewKMSKeySigner returns a new KMSKeySigner for the given Cloud KMS key.
func NewKMSKeySigner(
	ctx context.Context,
	key cloudkms.Key,
	logger zerolog.Logger,
) (*KMSKeySigner, error) {
	logger = logger.With().Str("component", "cloud_kms_signer").Logger()

	kmsClient, err := cloudkms.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to create Cloud KMS client: %w", err)
	}

	kmsSigner, err := kmsClient.SignerForKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf(
			"could not create KMS signer for the key with ID: %s: %w",
			key.KeyID,
			err,
		)
	}
	logger.Info().Str("public-key", kmsSigner.PublicKey().String()).Msg("KMS signer added")

	return &KMSKeySigner{
		kmsSigner: kmsSigner,
		logger:    logger,
	}, nil
}

// Sign signs the given message using the KMS signing key for this signer.
//
// Reference: https://cloud.google.com/kms/docs/create-validate-signatures
func (s *KMSKeySigner) Sign(message []byte) ([]byte, error) {
	defer func(start time.Time) {
		elapsed := time.Since(start)
		s.logger.Debug().
			Int64("duration", elapsed.Milliseconds()).
			Msg("messaged was signed")
	}(time.Now())

	signature, err := s.kmsSigner.Sign(message)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to sign message with public key %s: %w",
			s.kmsSigner.PublicKey(),
			err,
		)
	}

	return signature, err
}

// PublicKey returns the current public key of the Cloud KMS key.
func (s *KMSKeySigner) PublicKey() crypto.PublicKey {
	return s.kmsSigner.PublicKey()
}
