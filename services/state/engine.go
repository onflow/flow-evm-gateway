package state

import (
	"context"

	"github.com/google/uuid"
	"github.com/onflow/atree"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ models.Engine = &Engine{}
var _ models.Subscriber = &Engine{}

type Engine struct {
	config         *config.Config
	logger         zerolog.Logger
	status         *models.EngineStatus
	blockPublisher *models.Publisher
	blocks         storage.BlockIndexer
	transactions   storage.TransactionIndexer
	receipts       storage.ReceiptIndexer
	ledger         atree.Ledger
}

func NewStateEngine(
	config *config.Config,
	ledger atree.Ledger,
	blockPublisher *models.Publisher,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) *Engine {
	log := logger.With().Str("component", "state").Logger()

	return &Engine{
		config:         config,
		logger:         log,
		status:         models.NewEngineStatus(),
		blockPublisher: blockPublisher,
		blocks:         blocks,
		transactions:   transactions,
		receipts:       receipts,
		ledger:         ledger,
	}
}

// todo rethink whether it would be more robust to rely on blocks in the storage
// instead of receiving events, relying on storage and keeping a separate count of
// transactions executed would allow for independent restart and reexecution
// if we panic with events the missed tx won't get reexecuted since it's relying on
// event ingestion also not indexing that transaction

func (e *Engine) Notify(data any) {
	block, ok := data.(*models.Block)
	if !ok {
		e.logger.Error().Msg("invalid event type sent to state ingestion")
		return
	}

	e.logger.Info().Uint64("evm-height", block.Height).Msg("received new block")

	state, err := NewState(block, e.ledger, e.config.FlowNetworkID, e.blocks, e.receipts, e.logger)
	if err != nil {
		panic(err) // todo refactor
	}

	for _, h := range block.TransactionHashes {
		e.logger.Info().Str("hash", h.String()).Msg("transaction execution")

		tx, err := e.transactions.Get(h)
		if err != nil {
			panic(err) // todo refactor
		}

		err = state.Execute(tx)
		if err != nil {
			panic(err)
		}
	}
}

func (e *Engine) Run(ctx context.Context) error {
	e.blockPublisher.Subscribe(e)
	e.status.MarkReady()
	return nil
}

func (e *Engine) Stop() {
	// todo cleanup
	e.status.MarkStopped()
}

func (e *Engine) Done() <-chan struct{} {
	return e.status.IsDone()
}

func (e *Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}

func (e *Engine) Error() <-chan error {
	return nil
}

func (e *Engine) ID() uuid.UUID {
	return uuid.New()
}
