package bootstrap

import (
	"context"
	"fmt"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/rs/zerolog"
)

// createSigner creates the signer based on either a single coa key being
// provided and using a simple in-memory signer, or a Cloud KMS key being
// provided and using a Cloud KMS signer.
func createSigner(
	ctx context.Context,
	config config.Config,
	logger zerolog.Logger,
) (crypto.Signer, error) {
	var signer crypto.Signer
	var err error
	switch {
	case config.COAKey != nil:
		signer, err = crypto.NewInMemorySigner(config.COAKey, crypto.SHA2_256)
	case config.COACloudKMSKey != nil:
		signer, err = requester.NewKMSKeySigner(
			ctx,
			*config.COACloudKMSKey,
			logger,
		)
	default:
		return nil, fmt.Errorf("must provide either single COA / Cloud KMS key")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create a COA signer: %w", err)
	}

	return signer, nil
}
