package requester

import (
	"context"
	"fmt"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/module/component"
	"github.com/onflow/flow-go/module/irrecoverable"
	"github.com/rs/zerolog"
)

type KeyStoreComponent struct {
	*KeyStore

	log              zerolog.Logger
	config           config.Config
	client           access.Client
	startupCompleted chan struct{}
}

var _ component.Component = (*KeyStoreComponent)(nil)

func NewKeyStoreComponent(log zerolog.Logger, config config.Config, client access.Client) *KeyStoreComponent {
	ks := &KeyStoreComponent{
		log:              log,
		config:           config,
		client:           client,
		startupCompleted: make(chan struct{}),
	}

	return ks
}

func (k *KeyStoreComponent) Start(ctx irrecoverable.SignalerContext) {
	defer close(k.startupCompleted)

	k.log.Info().Msg("starting key store component")

	accountKeys := make([]*AccountKey, 0)
	account, err := k.client.GetAccount(ctx, k.config.COAAddress)
	if err != nil {
		ctx.Throw(fmt.Errorf(
			"failed to get signer info account for address: %s, with: %w",
			k.config.COAAddress,
			err,
		))

		return
	}
	signer, err := createSigner(ctx, k.config, k.log)

	if err != nil {
		ctx.Throw(err)

		return
	}
	for _, key := range account.Keys {
		accountKeys = append(accountKeys, &AccountKey{
			AccountKey: *key,
			Address:    k.config.COAAddress,
			Signer:     signer,
		})
	}

	k.KeyStore = NewKeyStore(accountKeys)

}

func (k *KeyStoreComponent) Ready() <-chan struct{} {
	ready := make(chan struct{})

	go func() {
		<-k.startupCompleted
		close(ready)
	}()

	return ready
}

func (k *KeyStoreComponent) Done() <-chan struct{} {
	done := make(chan struct{})

	go func() {
		<-k.startupCompleted

		// This is where we would close the KMS client connection,
		// but it currently does not have a close method

		close(done)
	}()

	return done
}

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
		signer, err = crypto.NewInMemorySigner(config.COAKey, crypto.SHA3_256)
	case config.COACloudKMSKey != nil:
		signer, err = NewKMSKeySigner(
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
