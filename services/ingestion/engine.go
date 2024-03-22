package ingestion

import (
	"context"
	"errors"
	"fmt"
	"github.com/onflow/flow-go/engine"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"
)

var _ models.Engine = &Engine{}

type Engine struct {
	subscriber        EventSubscriber
	blocks            storage.BlockIndexer
	receipts          storage.ReceiptIndexer
	transactions      storage.TransactionIndexer
	accounts          storage.AccountIndexer
	log               zerolog.Logger
	evmLastHeight     *models.SequentialHeight
	status            *models.EngineStatus
	blocksBroadcaster *engine.Broadcaster
}

func NewEventIngestionEngine(
	subscriber EventSubscriber,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	transactions storage.TransactionIndexer,
	accounts storage.AccountIndexer,
	blocksBroadcaster *engine.Broadcaster,
	log zerolog.Logger,
) *Engine {
	log = log.With().Str("component", "ingestion").Logger()

	return &Engine{
		subscriber:        subscriber,
		blocks:            blocks,
		receipts:          receipts,
		transactions:      transactions,
		accounts:          accounts,
		log:               log,
		status:            models.NewEngineStatus(),
		blocksBroadcaster: blocksBroadcaster,
	}
}

// Ready signals when the engine has started.
func (e *Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}

// Done signals when the engine has stopped.
func (e *Engine) Done() <-chan struct{} {
	// return e.status.IsDone()
	return nil
}

// Stop the engine.
func (e *Engine) Stop() {
	// todo
}

// Run the event ingestion engine. Load the latest height that was stored and provide it
// to the event subscribers as a starting point.
// Consume the events provided by the event subscriber.
// Each event is then processed by the event processing methods.
func (e *Engine) Run(ctx context.Context) error {
	latestCadence, err := e.blocks.LatestCadenceHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest cadence height: %w", err)
	}

	e.log.Info().Uint64("start-cadence-height", latestCadence).Msg("starting ingestion")

	events, errs, err := e.subscriber.Subscribe(ctx, latestCadence)
	if err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	e.status.MarkReady()

	for {
		select {
		case <-ctx.Done():
			e.log.Info().Msg("event ingestion received done signal")
			return nil

		case blockEvents, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return models.ErrDisconnected
			}

			err = e.processEvents(blockEvents)
			if err != nil {
				e.log.Error().Err(err).Msg("failed to process EVM events")
				return err
			}

		case err, ok := <-errs:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				return models.ErrDisconnected
			}

			return errors.Join(err, models.ErrDisconnected)
		}
	}
}

// processEvents iterates all the events and decides based on the type how to process them.
func (e *Engine) processEvents(events flow.BlockEvents) error {
	e.log.Debug().
		Uint64("cadence-height", events.Height).
		Int("cadence-event-length", len(events.Events)).
		Msg("received new cadence evm events")

	blockEvent := false
	for _, event := range events.Events {
		var err error
		switch {
		case models.IsBlockExecutedEvent(event.Value):
			err = e.processBlockEvent(events.Height, event.Value)
			blockEvent = true
		case models.IsTransactionExecutedEvent(event.Value):
			err = e.processTransactionEvent(event.Value)
		default:
			return fmt.Errorf("invalid event type") // should never happen
		}

		if err != nil {
			return fmt.Errorf("failed to process event: %w", err)
		}
	}

	// this is an optimization, because if there is a block event in batch, it will internally update latest height
	// otherwise we still want to update it explicitly, so we don't have to reindex in case of a restart
	if !blockEvent {
		if err := e.blocks.SetLatestCadenceHeight(events.Height); err != nil {
			return fmt.Errorf("failed to update to latest cadence height during events ingestion: %w", err)
		}
	}

	return nil
}

func (e *Engine) processBlockEvent(cadenceHeight uint64, event cadence.Event) error {
	block, err := models.DecodeBlock(event)
	if err != nil {
		return fmt.Errorf("could not decode block event: %w", err)
	}

	// only init latest height if not set
	if e.evmLastHeight == nil {
		e.evmLastHeight = models.NewSequentialHeight(block.Height)
	}

	// make sure the latest height is increasing sequentially or is same as latest
	if err = e.evmLastHeight.Increment(block.Height); err != nil {
		return fmt.Errorf("invalid block height, expected %d, got %d: %w", e.evmLastHeight.Load(), block.Height, err)
	}

	h, _ := block.Hash()
	e.log.Info().
		Str("hash", h.Hex()).
		Uint64("evm-height", block.Height).
		Str("parent-hash", block.ParentBlockHash.String()).
		Str("tx-hash", block.TransactionHashes[0].Hex()). // now we only have 1 tx per block
		Msg("new evm block executed event")

	if err := e.blocks.Store(cadenceHeight, block); err != nil {
		return err
	}

	e.blocksBroadcaster.Publish()
	return nil
}

func (e *Engine) processTransactionEvent(event cadence.Event) error {
	tx, err := models.DecodeTransaction(event)
	if err != nil {
		return fmt.Errorf("could not decode transaction event: %w", err)
	}

	receipt, err := models.DecodeReceipt(event)
	if err != nil {
		return fmt.Errorf("failed to decode receipt: %w", err)
	}

	// TODO(m-Peter): Remove the error return value once flow-go is updated
	txHash, err := tx.Hash()
	if err != nil {
		return fmt.Errorf("failed to compute TX hash: %w", err)
	}

	e.log.Info().
		Str("contract-address", receipt.ContractAddress.String()).
		Int("log-count", len(receipt.Logs)).
		Str("receipt-tx-hash", receipt.TxHash.String()).
		Str("tx-hash", txHash.String()).
		Msg("ingesting new transaction executed event")

	// todo think if we could introduce batching
	if err := e.transactions.Store(tx); err != nil {
		return fmt.Errorf("failed to store tx: %w", err)
	}

	if err := e.accounts.Update(tx, receipt); err != nil {
		return fmt.Errorf("failed to update accounts: %w", err)
	}

	if err := e.receipts.Store(receipt); err != nil {
		return fmt.Errorf("failed to store receipt: %w", err)
	}

	return nil
}
