package ingestion

import (
	"context"
	"errors"
	"fmt"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"
)

var ErrDisconnected = errors.New("disconnected")

var _ models.Engine = &Engine{}

type Engine struct {
	subscriber    EventSubscriber
	blocks        storage.BlockIndexer
	receipts      storage.ReceiptIndexer
	transactions  storage.TransactionIndexer
	accounts      storage.AccountIndexer
	log           zerolog.Logger
	evmLastHeight *models.SequentialHeight
	status        *models.EngineStatus
}

func NewEventIngestionEngine(
	subscriber EventSubscriber,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	transactions storage.TransactionIndexer,
	accounts storage.AccountIndexer,
	log zerolog.Logger,
) *Engine {
	log = log.With().Str("component", "ingestion").Logger()

	return &Engine{
		subscriber:   subscriber,
		blocks:       blocks,
		receipts:     receipts,
		transactions: transactions,
		accounts:     accounts,
		log:          log,
		status:       models.NewEngineStatus(),
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

// Start the event ingestion engine. Load the latest height that was stored and provide it
// to the event subscribers as a starting point.
// Consume the events provided by the event subscriber.
// Each event is then processed by the event processing methods.
func (e *Engine) Start(ctx context.Context) error {
	latestCadence, err := e.blocks.LatestCadenceHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest cadence height: %w", err)
	}

	// todo we increase the latest indexed cadence height by one, but this assumes the last index of data
	// was successful, in case of crash we would need to reindex. We do this now because nonces are not
	// mapped to heights, which means the update to account is not idempotent, once update to account is
	// idempotent we should remove this increment by 1.
	latestCadence += 1

	e.log.Info().Uint64("start-cadence-height", latestCadence).Msg("starting ingestion")

	events, errs, err := e.subscriber.Subscribe(ctx, latestCadence)
	if err != nil {
		return err
	}

	e.status.Ready()

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
				return ErrDisconnected
			}

			err = e.processEvents(blockEvents)
			if err != nil {
				return err
			}

		case err, ok := <-errs:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				return ErrDisconnected
			}

			return errors.Join(err, ErrDisconnected)
		}
	}
}

// processEvents iterates all the events and decides based on the type how to process them.
func (e *Engine) processEvents(events flow.BlockEvents) error {
	e.log.Debug().
		Uint64("cadence-height", events.Height).
		Int("cadence-event-length", len(events.Events)).
		Msg("received new cadence evm events")

	for _, event := range events.Events {
		if models.IsBlockExecutedEvent(event.Value) {
			err := e.processBlockEvent(events.Height, event.Value)
			if err != nil {
				return err
			}
		} else if models.IsTransactionExecutedEvent(event.Value) {
			err := e.processTransactionEvent(event.Value)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("invalid event type") // should never happen
		}
	}

	return nil
}

func (e *Engine) processBlockEvent(cadenceHeight uint64, event cadence.Event) error {
	block, err := models.DecodeBlock(event)
	if err != nil {
		return err
	}

	// only init latest height if not set
	if e.evmLastHeight == nil {
		e.evmLastHeight = models.NewSequentialHeight(block.Height)
	} else { // otherwise make sure the latest height is same as the one set on the engine
		err = e.evmLastHeight.Increment(block.Height)
		if err != nil {
			return err
		}
	}

	h, _ := block.Hash()
	e.log.Info().
		Str("hash", h.Hex()).
		Uint64("evm-height", block.Height).
		Str("parent-hash", block.ParentBlockHash.String()).
		Str("tx-hash", block.TransactionHashes[0].Hex()). // now we only have 1 tx per block
		Msg("new evm block executed event")

	if err = e.evmLastHeight.Increment(block.Height); err != nil {
		return fmt.Errorf("invalid block height, expected %d, got %d: %w", e.evmLastHeight.Load(), block.Height, err)
	}

	return e.blocks.Store(cadenceHeight, block)
}

func (e *Engine) processTransactionEvent(event cadence.Event) error {
	tx, err := models.DecodeTransaction(event)
	if err != nil {
		return err
	}

	// in case we have a direct call transaction we ignore it for now
	// todo support indexing of direct calls
	if tx == nil {
		return nil
	}

	receipt, err := models.DecodeReceipt(event)
	if err != nil {
		return err
	}

	e.log.Info().
		Str("contract-address", receipt.ContractAddress.String()).
		Int("log-count", len(receipt.Logs)).
		Str("receipt-tx-hash", receipt.TxHash.String()).
		Str("tx hash", tx.Hash().String()).
		Msg("ingesting new transaction executed event")

	// todo think if we could introduce batching
	if err := e.transactions.Store(tx); err != nil {
		return err
	}

	if err := e.accounts.Update(tx); err != nil {
		return err
	}

	if err := e.receipts.Store(receipt); err != nil {
		return err
	}

	return nil
}
