package ingestion

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// RPCBlockTrackingSubscriber subscribes to new EVM block events for unsealed finalized blocks.
// This is accomplished by following finalized blocks from the upstream Access node, and using the
// polling endpoint to fetch the events for each finalized block.
//
// IMPORTANT: Since data is downloaded and processed from unsealed blocks, it's possible for the
// data that was downloaded to be incorrect. This subscriber provides no handling or detection for
// cases where the received data differs from the data that was ultimately sealed. The operator must
// handle this manually.
// Since it's not reasonable to expect operators to do this manual tracking, this features should NOT
// be used outside of a limited Proof of Concept. Use at own risk.
//
// A future version of the RPCEventSubscriber will provide this detection and handling functionality
// at which point this subscriber will be removed.
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

	var blockHeadersChan <-chan flow.BlockHeader
	var errChan <-chan error

	lastReceivedHeight := height
	connect := func(height uint64) error {
		var err error
		blockHeadersChan, errChan, err = r.client.SubscribeBlockHeadersFromStartHeight(
			ctx,
			height,
			flow.BlockStatusFinalized,
		)
		return err
	}

	if err := connect(lastReceivedHeight); err != nil {
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
					// typically we receive an error in the errChan before the channels are closes
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewBlockEventsError(err)
					return
				}

				blockEvents, err := r.evmEventsForBlock(ctx, blockHeader)
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
					// typically we receive an error in the errChan before the channels are closes
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewBlockEventsError(err)
					return
				}

				switch status.Code(err) {
				case codes.NotFound:
					// we can get not found when reconnecting after a disconnect/restart before the
					// next block is finalized. just wait briefly and try again
					time.Sleep(200 * time.Millisecond)
				case codes.DeadlineExceeded, codes.Internal:
					// these are sometimes returned when the stream is disconnected by a middleware or the server
				default:
					// skip reconnect on all other errors
					eventsChan <- models.NewBlockEventsError(fmt.Errorf("%w: %w", errs.ErrDisconnected, err))
					return
				}

				if err := connect(lastReceivedHeight + 1); err != nil {
					eventsChan <- models.NewBlockEventsError(
						fmt.Errorf(
							"failed to resubscribe for finalized block headers on height: %d, with: %w",
							lastReceivedHeight+1,
							err,
						),
					)
					return
				}
			}
		}
	}()

	return eventsChan
}

func (r *RPCBlockTrackingSubscriber) evmEventsForBlock(
	ctx context.Context,
	blockHeader flow.BlockHeader,
) (flow.BlockEvents, error) {
	eventTypes := blocksFilter(r.chain).EventTypes

	// evm Block events
	blockEvents, err := r.getEventsByType(ctx, blockHeader, eventTypes[0])
	if err != nil {
		return flow.BlockEvents{}, err
	}

	payload, err := events.DecodeBlockEventPayload(blockEvents.Events[0].Value)
	if err != nil {
		return flow.BlockEvents{}, err
	}

	if payload.TransactionHashRoot == types.EmptyTxsHash {
		return blockEvents, nil
	}

	// evm TX events
	txEvents, err := r.getEventsByType(ctx, blockHeader, eventTypes[1])
	if err != nil {
		return flow.BlockEvents{}, err
	}

	// combine block and tx events to be processed together
	blockEvents.Events = append(blockEvents.Events, txEvents.Events...)

	return blockEvents, nil
}

func (r *RPCBlockTrackingSubscriber) getEventsByType(
	ctx context.Context,
	blockHeader flow.BlockHeader,
	eventType string,
) (flow.BlockEvents, error) {
	var evts []flow.BlockEvents
	var err error

	// retry until we get the block from an execution node that has the events
	for {
		evts, err = r.client.GetEventsForBlockHeader(
			ctx,
			eventType,
			blockHeader,
		)
		if err != nil {
			// retry after a short pause
			if status.Code(err) == codes.NotFound || status.Code(err) == codes.ResourceExhausted {
				time.Sleep(200 * time.Millisecond)
				continue
			}

			return flow.BlockEvents{}, err
		}
		break
	}

	if len(evts) != 1 {
		// this shouldn't happen and probably indicates a bug on the Access node.
		return flow.BlockEvents{}, fmt.Errorf(
			"received unexpected number of block events: got: %d, expected: 1",
			len(evts),
		)
	}

	// The `EVM.BlockExecuted` event should be present for every Flow block.
	if strings.Contains(eventType, string(events.EventTypeBlockExecuted)) {
		if len(evts[0].Events) != 1 {
			return flow.BlockEvents{}, fmt.Errorf(
				"received unexpected number of EVM events in block: got: %d, expected: 1",
				len(evts[0].Events),
			)
		}
	}

	return evts[0], nil
}
