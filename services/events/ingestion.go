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

type EventIngestionEngine struct {
	subscriber   Subscriber
	blocks       storage.BlockIndexer
	receipts     storage.ReceiptIndexer
	transactions storage.TransactionIndexer
	logs         zerolog.Logger
}

// Ready signals when the engine has started.
func (e *EventIngestionEngine) Ready() <-chan struct{} {
	// todo
	return nil
}

// Done signals when the engine has stopped.
func (e *EventIngestionEngine) Done() <-chan struct{} {
	// todo
	return nil
}

// Stop the engine.
func (e *EventIngestionEngine) Stop() {
}

func (e *EventIngestionEngine) Start(ctx context.Context) error {
	latest, err := e.blocks.LatestHeight()
	if err != nil {
		return err
	}
	startFrom := latest + 1
	e.logs.Info().Uint64("start height", startFrom).Msg("starting ingestion")

	events, errs, err := e.subscriber.Subscribe(ctx, startFrom)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
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

			return err
		}
	}
}

func (e *EventIngestionEngine) processEvents(events flow.BlockEvents) error {
	// todo validate events.Height is sequential

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

	return e.blocks.Store(block)
}

func (e *EventIngestionEngine) processTransactionEvent(event cadence.Event) error {
	tx, err := models.DecodeTransaction(event)
	if err != nil {
		return err
	}

	receipt, err := models.DecodeReceipt(event)
	if err != nil {
		return err
	}

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
