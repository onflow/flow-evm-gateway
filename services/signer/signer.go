package signer

import (
	"fmt"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/module/component"
	"github.com/onflow/flow-go/module/irrecoverable"
	"github.com/rs/zerolog"
)

type Signer struct {
	crypto.Signer

	log    zerolog.Logger
	config config.Config

	startupCompleted chan struct{}
	closeKMSClient   func()
}

var _ component.Component = (*Signer)(nil)
var _ crypto.Signer = (*Signer)(nil)

func NewSigner(log zerolog.Logger, config config.Config) *Signer {
	return &Signer{
		log:              log,
		config:           config,
		startupCompleted: make(chan struct{}),
	}
}

func (s *Signer) Start(ctx irrecoverable.SignalerContext) {
	cfg := s.config
	defer close(s.startupCompleted)

	var err error
	switch {
	case cfg.COAKey != nil:
		s.Signer, err = crypto.NewInMemorySigner(cfg.COAKey, crypto.SHA3_256)
	case cfg.COAKeys != nil:
		s.Signer, err = requester.NewKeyRotationSigner(cfg.COAKeys, crypto.SHA3_256)
	case len(cfg.COACloudKMSKeys) > 0:
		var signer *requester.KMSKeyRotationSigner
		signer, err = requester.NewKMSKeyRotationSigner(
			ctx,
			cfg.COACloudKMSKeys,
			s.log,
		)
		s.closeKMSClient = func() {
			// TODO(JanezP): this should definitely be a closer. Open a PR in the sdk
			// signer.Close()
		}
		s.Signer = signer
	default:
		ctx.Throw(fmt.Errorf("must provide either single COA / keylist of COA keys / COA cloud KMS keys"))
		return
	}
	if err != nil {
		ctx.Throw(fmt.Errorf("failed to create a COA signer: %w", err))
		return
	}
}

func (s *Signer) Ready() <-chan struct{} {
	ready := make(chan struct{})

	go func() {
		<-s.startupCompleted
		close(ready)
	}()

	return ready
}

func (s *Signer) Done() <-chan struct{} {
	done := make(chan struct{})

	go func() {
		<-s.startupCompleted

		if s.closeKMSClient != nil {
			s.closeKMSClient()
		}

		close(done)
	}()

	return done
}
