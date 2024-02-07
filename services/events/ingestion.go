package events

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

var _ models.Engine = &EventIngestionEngine{}

type EventIngestionEngine struct {
	subscriber   Subscriber
	blocks       storage.BlockIndexer
	receipts     storage.ReceiptIndexer
	transactions storage.TransactionIndexer
	logs         zerolog.Logger
	lastHeight   *models.SequentialHeight
	status       *models.EngineStatus
}

func NewEventIngestionEngine(
	subscriber Subscriber,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	transactions storage.TransactionIndexer,
	logs zerolog.Logger,
) *EventIngestionEngine {
	return &EventIngestionEngine{
		subscriber:   subscriber,
		blocks:       blocks,
		receipts:     receipts,
		transactions: transactions,
		logs:         logs,
		status:       models.NewEngineStatus(),
	}
}

// Ready signals when the engine has started.
func (e *EventIngestionEngine) Ready() <-chan struct{} {
	return e.status.IsReady()
}

// Done signals when the engine has stopped.
func (e *EventIngestionEngine) Done() <-chan struct{} {
	// return e.status.IsDone()
	return nil
}

// Stop the engine.
func (e *EventIngestionEngine) Stop() {
	// todo
}

// Start the event ingestion engine. Load the latest height that was stored and provide it
// to the event subscribers as a starting point.
// Consume the events provided by the event subscriber.
// Each event is then processed by the event processing methods.
func (e *EventIngestionEngine) Start(ctx context.Context) error {
	latest, err := e.blocks.LatestHeight()
	if err != nil {
		return err
	}

	// only init latest height if not set
	if e.lastHeight == nil {
		e.lastHeight = models.NewSequentialHeight(latest)
	} else { // otherwise make sure the latest height is same as the one set on the engine
		err = e.lastHeight.Increment(latest)
		if err != nil {
			return err
		}
	}

	e.logs.Info().Uint64("start height", latest).Msg("starting ingestion")

	events, errs, err := e.subscriber.Subscribe(ctx, latest)
	if err != nil {
		return err
	}

	e.status.Ready()

	for {
		select {
		case <-ctx.Done():
			e.logs.Info().Msg("event ingestion received done signal")
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
func (e *EventIngestionEngine) processEvents(events flow.BlockEvents) error {
	e.logs.Debug().
		Uint64("cadence height", events.Height).
		Int("cadence event length", len(events.Events)).
		Msg("received new cadence evm events")

	for _, event := range events.Events {
		if models.IsBlockExecutedEvent(event.Value) {
			err := e.processBlockEvent(event.Value)
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

func (e *EventIngestionEngine) processBlockEvent(event cadence.Event) error {
	block, err := models.DecodeBlock(event)
	if err != nil {
		return err
	}

	e.logs.Info().
		Uint64("evm height", block.Height).
		Str("parent hash", block.ParentBlockHash.String()).
		Msg("new evm block executed event")

	if err = e.lastHeight.Increment(block.Height); err != nil {
		return fmt.Errorf("invalid block height, expected %d, got %d: %w", e.lastHeight.Load(), block.Height, err)
	}

	return e.blocks.Store(block)
}

func (e *EventIngestionEngine) processTransactionEvent(event cadence.Event) error {
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

	e.logs.Info().
		Str("contract address", receipt.ContractAddress.String()).
		Int("log count", len(receipt.Logs)).
		Str("receipt tx hash", receipt.TxHash.String()).
		Str("tx hash", tx.Hash().String()).
		Msg("ingesting new transaction executed event")

	err = e.transactions.Store(tx)
	if err != nil {
		return err
	}

	err = e.receipts.Store(receipt)
	if err != nil {
		return err
	}

	return nil
}
