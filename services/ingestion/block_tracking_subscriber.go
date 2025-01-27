package ingestion

import (
	"context"
	"fmt"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/events"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ EventSubscriber = &RPCBlockTrackingSubscriber{}

type RPCBlockTrackingSubscriber struct {
	*RPCEventSubscriber
}

func NewRPCBlockTrackingSubscriber(
	logger zerolog.Logger,
	client *requester.CrossSporkClient,
	chainID flowGo.ChainID,
	keyLock requester.KeyLock,
	startHeight uint64,
) *RPCBlockTrackingSubscriber {
	return &RPCBlockTrackingSubscriber{
		RPCEventSubscriber: NewRPCEventSubscriber(
			logger.With().Str("component", "subscriber").Logger(),
			client,
			chainID,
			keyLock,
			startHeight,
		),
	}
}

// Subscribe will retrieve all the events from the provided height. If the height is from previous
// sporks, it will first backfill all the events in all the previous sporks, and then continue
// to listen all new events in the current spork.
//
// If error is encountered during backfill the subscription will end and the response chanel will be closed.
func (r *RPCBlockTrackingSubscriber) Subscribe(ctx context.Context) <-chan models.BlockEvents {
	// buffered channel so that the decoding of the events can happen in parallel to other operations
	eventsChan := make(chan models.BlockEvents, 1000)

	go func() {
		defer func() {
			close(eventsChan)
		}()

		// if the height is from the previous spork, backfill all the eventsChan from previous sporks first
		if r.client.IsPastSpork(r.height) {
			r.logger.Info().
				Uint64("height", r.height).
				Msg("height found in previous spork, starting to backfill")

			// backfill all the missed events, handling of context cancellation is done by the producer
			for ev := range r.backfill(ctx, r.height) {
				eventsChan <- ev

				if ev.Err != nil {
					return
				}

				// keep updating height, so after we are done back-filling
				// it will be at the first height in the current spork
				r.height = ev.Events.CadenceHeight()
			}

			// after back-filling is done, increment height by one,
			// so we start with the height in the current spork
			r.height = r.height + 1
		}

		r.logger.Info().
			Uint64("next-height", r.height).
			Msg("backfilling done, subscribe for live data")

		// subscribe in the current spork, handling of context cancellation is done by the producer
		for ev := range r.subscribe(ctx, r.height) {
			eventsChan <- ev
		}

		r.logger.Warn().Msg("ended subscription for events")
	}()

	return eventsChan
}

// subscribe to events by the provided height and handle any errors.
//
// Subscribing to EVM specific events and handle any disconnection errors
// as well as context cancellations.
func (r *RPCBlockTrackingSubscriber) subscribe(ctx context.Context, height uint64) <-chan models.BlockEvents {
	eventsChan := make(chan models.BlockEvents)

	blockHeadersChan, errChan, err := r.client.SubscribeBlockHeadersFromStartHeight(
		ctx,
		height,
		flow.BlockStatusFinalized,
	)
	if err != nil {
		eventsChan <- models.NewBlockEventsError(
			fmt.Errorf(
				"failed to subscribe for finalized block headers on height: %d, with: %w",
				height,
				err,
			),
		)
		close(eventsChan)
		return eventsChan
	}
	lastReceivedHeight := height

	go func() {
		defer func() {
			close(eventsChan)
		}()

		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				r.logger.Info().Msg("event ingestion received done signal")
				return

			case blockHeader, ok := <-blockHeadersChan:
				if !ok {
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewBlockEventsError(err)
					return
				}

				blockEvents, err := r.evmEventsForBlockHeader(ctx, blockHeader)
				if err != nil {
					eventsChan <- models.NewBlockEventsError(err)
					return
				}

				evmEvents := models.NewSingleBlockEvents(blockEvents)
				// if events contain an error, or we are in a recovery mode
				if evmEvents.Err != nil || r.recovery {
					evmEvents = r.recover(ctx, blockEvents, evmEvents.Err)
					// if we are still in recovery go to the next event
					if r.recovery {
						continue
					}
				}

				for _, evt := range blockEvents.Events {
					r.keyLock.UnlockKey(evt.TransactionID)
				}
				r.keyLock.Notify(blockHeader.Height)
				lastReceivedHeight = blockHeader.Height

				eventsChan <- evmEvents

			case err, ok := <-errChan:
				if !ok {
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewBlockEventsError(err)
					return
				}

				if status.Code(err) == codes.DeadlineExceeded || status.Code(err) == codes.Internal {
					blockHeadersChan, errChan, err = r.client.SubscribeBlockHeadersFromStartHeight(
						ctx,
						lastReceivedHeight+1,
						flow.BlockStatusFinalized,
					)
					if err != nil {
						eventsChan <- models.NewBlockEventsError(
							fmt.Errorf(
								"failed to subscribe for finalized block headers on height: %d, with: %w",
								height,
								err,
							),
						)
						return
					}
				} else {
					eventsChan <- models.NewBlockEventsError(fmt.Errorf("%w: %w", errs.ErrDisconnected, err))
					return
				}
			}
		}
	}()

	return eventsChan
}

func (r *RPCBlockTrackingSubscriber) evmEventsForBlockHeader(
	ctx context.Context,
	blockHeader flow.BlockHeader,
) (flow.BlockEvents, error) {
	eventTypes := blocksFilter(r.chain).EventTypes
	evmBlockEvent := eventTypes[0]

	evts, err := r.client.GetEventsForBlockHeader(
		ctx,
		evmBlockEvent,
		blockHeader,
	)
	if err != nil {
		return flow.BlockEvents{}, err
	}

	// We are requesting the `EVM.BlockExecuted` events for a single Flow block,
	// so we expect the length of `evts` to equal 1.
	// The `EVM.BlockExecuted` event should be present for every Flow block.
	if len(evts) != 1 || len(evts[0].Events) != 1 {
		return flow.BlockEvents{}, fmt.Errorf(
			"received unexpected number of EVM events for height: %d, got: %d, expected: 1",
			blockHeader.Height,
			len(evts),
		)
	}

	blockEvents := evts[0]
	payload, err := events.DecodeBlockEventPayload(blockEvents.Events[0].Value)
	if err != nil {
		return flow.BlockEvents{}, err
	}

	if payload.TransactionHashRoot == types.EmptyTxsHash {
		return blockEvents, nil
	}

	evmTxEvent := eventTypes[1]
	evts, err = r.client.GetEventsForBlockHeader(
		ctx,
		evmTxEvent,
		blockHeader,
	)
	if err != nil {
		return flow.BlockEvents{}, err
	}

	// We are requesting the `EVM.TransactionExecuted` events for a single
	// Flow block, so we expect the length of `evts` to equal 1.
	if len(evts) != 1 {
		return flow.BlockEvents{}, fmt.Errorf(
			"received unexpected number of EVM events for height: %d, got: %d, expected: 1",
			blockHeader.Height,
			len(evts),
		)
	}
	txEvents := evts[0]

	blockEvents.Events = append(blockEvents.Events, txEvents.Events...)

	return blockEvents, nil
}
