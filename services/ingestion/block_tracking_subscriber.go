package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/onflow/cadence/common"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
)

var _ EventSubscriber = &RPCBlockTrackingSubscriber{}

type RPCBlockTrackingSubscriber struct {
	logger zerolog.Logger

	client  *requester.CrossSporkClient
	chain   flowGo.ChainID
	keyLock requester.KeyLock
	height  uint64

	recovery        bool
	recoveredEvents []flow.Event
}

func NewRPCBlockTrackingSubscriber(
	logger zerolog.Logger,
	client *requester.CrossSporkClient,
	chainID flowGo.ChainID,
	keyLock requester.KeyLock,
	startHeight uint64,
) *RPCBlockTrackingSubscriber {
	logger = logger.With().Str("component", "subscriber").Logger()
	return &RPCBlockTrackingSubscriber{
		logger: logger,

		client:  client,
		chain:   chainID,
		keyLock: keyLock,
		height:  startHeight,
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

				var blockEvents flow.BlockEvents
				for _, eventType := range blocksFilter(r.chain).EventTypes {
					evts, err := r.client.GetEventsForHeightRange(
						ctx,
						eventType,
						blockHeader.Height,
						blockHeader.Height,
					)
					if err != nil {
						eventsChan <- models.NewBlockEventsError(
							fmt.Errorf(
								"failed to fetch EVM events for height: %d, with: %w",
								blockHeader.Height,
								err,
							),
						)
						return
					}

					if len(evts) != 1 {
						eventsChan <- models.NewBlockEventsError(
							fmt.Errorf(
								"received unexpected number of EVM events for height: %d, got: %d, expected: 1",
								blockHeader.Height,
								len(evts),
							),
						)
						return
					}
					blockEvent := evts[0]
					blockEvents.Events = append(blockEvents.Events, blockEvent.Events...)
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

				if strings.Contains(errors.Unwrap(err).Error(), "DeadlineExceeded") ||
					strings.Contains(errors.Unwrap(err).Error(), "unexpected EOF") {
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

// backfill returns a channel that is filled with block events from the provided fromCadenceHeight up to the first
// height in the current spork.
func (r *RPCBlockTrackingSubscriber) backfill(ctx context.Context, fromCadenceHeight uint64) <-chan models.BlockEvents {
	eventsChan := make(chan models.BlockEvents)

	go func() {
		defer func() {
			close(eventsChan)
		}()

		for {
			// check if the current fromCadenceHeight is still in past sporks, and if not return since we are done with backfilling
			if !r.client.IsPastSpork(fromCadenceHeight) {
				r.logger.Info().
					Uint64("height", fromCadenceHeight).
					Msg("completed backfilling")

				return
			}

			var err error
			fromCadenceHeight, err = r.backfillSporkFromHeight(ctx, fromCadenceHeight, eventsChan)
			if err != nil {
				r.logger.Error().Err(err).Msg("error backfilling spork")
				eventsChan <- models.NewBlockEventsError(err)
				return
			}

			r.logger.Info().
				Uint64("next-cadence-height", fromCadenceHeight).
				Msg("reached the end of spork, checking next spork")
		}
	}()

	return eventsChan
}

// / backfillSporkFromHeight will fill the eventsChan with block events from the provided fromHeight up to the first height in the spork that comes
// after the spork of the provided fromHeight.
func (r *RPCBlockTrackingSubscriber) backfillSporkFromHeight(ctx context.Context, fromCadenceHeight uint64, eventsChan chan<- models.BlockEvents) (uint64, error) {
	evmAddress := common.Address(systemcontracts.SystemContractsForChain(r.chain).EVMContract.Address)

	blockExecutedEvent := common.NewAddressLocation(
		nil,
		evmAddress,
		string(events.EventTypeBlockExecuted),
	).ID()

	transactionExecutedEvent := common.NewAddressLocation(
		nil,
		evmAddress,
		string(events.EventTypeTransactionExecuted),
	).ID()

	lastHeight, err := r.client.GetLatestHeightForSpork(ctx, fromCadenceHeight)
	if err != nil {
		eventsChan <- models.NewBlockEventsError(err)
		return 0, err
	}

	r.logger.Info().
		Uint64("start-height", fromCadenceHeight).
		Uint64("last-spork-height", lastHeight).
		Msg("backfilling spork")

	for fromCadenceHeight < lastHeight {
		r.logger.Debug().Msg(fmt.Sprintf("backfilling [%d / %d] ...", fromCadenceHeight, lastHeight))

		startHeight := fromCadenceHeight
		endHeight := fromCadenceHeight + maxRangeForGetEvents
		if endHeight > lastHeight {
			endHeight = lastHeight
		}

		blocks, err := r.client.GetEventsForHeightRange(ctx, blockExecutedEvent, startHeight, endHeight)
		if err != nil {
			return 0, fmt.Errorf("failed to get block events: %w", err)
		}

		transactions, err := r.client.GetEventsForHeightRange(ctx, transactionExecutedEvent, startHeight, endHeight)
		if err != nil {
			return 0, fmt.Errorf("failed to get block events: %w", err)
		}

		if len(transactions) != len(blocks) {
			return 0, fmt.Errorf("transactions and blocks have different length")
		}

		// sort both, just in case
		sort.Slice(blocks, func(i, j int) bool {
			return blocks[i].Height < blocks[j].Height
		})
		sort.Slice(transactions, func(i, j int) bool {
			return transactions[i].Height < transactions[j].Height
		})

		for i := range transactions {
			if transactions[i].Height != blocks[i].Height {
				return 0, fmt.Errorf("transactions and blocks have different height")
			}

			// append the transaction events to the block events
			blocks[i].Events = append(blocks[i].Events, transactions[i].Events...)

			evmEvents := models.NewSingleBlockEvents(blocks[i])
			if evmEvents.Err != nil && errors.Is(evmEvents.Err, errs.ErrMissingBlock) {
				evmEvents, err = r.accumulateBlockEvents(
					ctx,
					blocks[i],
					blockExecutedEvent,
					transactionExecutedEvent,
				)
				if err != nil {
					return 0, err
				}
				eventsChan <- evmEvents
				// advance the height
				fromCadenceHeight = evmEvents.Events.CadenceHeight() + 1
				break
			}
			eventsChan <- evmEvents

			// advance the height
			fromCadenceHeight = evmEvents.Events.CadenceHeight() + 1
		}

	}
	return fromCadenceHeight, nil
}

// accumulateBlockEvents will keep fetching `EVM.TransactionExecuted` events
// until it finds their `EVM.BlockExecuted` event.
// At that point it will return the valid models.BlockEvents.
func (r *RPCBlockTrackingSubscriber) accumulateBlockEvents(
	ctx context.Context,
	block flow.BlockEvents,
	blockExecutedEventType string,
	txExecutedEventType string,
) (models.BlockEvents, error) {
	evmEvents := models.NewSingleBlockEvents(block)
	currentHeight := block.Height
	transactionEvents := make([]flow.Event, 0)

	for evmEvents.Err != nil && errors.Is(evmEvents.Err, errs.ErrMissingBlock) {
		blocks, err := r.client.GetEventsForHeightRange(
			ctx,
			blockExecutedEventType,
			currentHeight,
			currentHeight+maxRangeForGetEvents,
		)
		if err != nil {
			return models.BlockEvents{}, fmt.Errorf("failed to get block events: %w", err)
		}

		transactions, err := r.client.GetEventsForHeightRange(
			ctx,
			txExecutedEventType,
			currentHeight,
			currentHeight+maxRangeForGetEvents,
		)
		if err != nil {
			return models.BlockEvents{}, fmt.Errorf("failed to get block events: %w", err)
		}

		if len(transactions) != len(blocks) {
			return models.BlockEvents{}, fmt.Errorf("transactions and blocks have different length")
		}

		// sort both, just in case
		sort.Slice(blocks, func(i, j int) bool {
			return blocks[i].Height < blocks[j].Height
		})
		sort.Slice(transactions, func(i, j int) bool {
			return transactions[i].Height < transactions[j].Height
		})

		for i := range blocks {
			if transactions[i].Height != blocks[i].Height {
				return models.BlockEvents{}, fmt.Errorf("transactions and blocks have different height")
			}

			// If no EVM.BlockExecuted event found, keep accumulating the incoming
			// EVM.TransactionExecuted events, until we find the EVM.BlockExecuted
			// event that includes them.
			if len(blocks[i].Events) == 0 {
				txEvents := transactions[i].Events
				// Sort `EVM.TransactionExecuted` events
				sort.Slice(txEvents, func(i, j int) bool {
					if txEvents[i].TransactionIndex != txEvents[j].TransactionIndex {
						return txEvents[i].TransactionIndex < txEvents[j].TransactionIndex
					}
					return txEvents[i].EventIndex < txEvents[j].EventIndex
				})
				transactionEvents = append(transactionEvents, txEvents...)
			} else {
				blocks[i].Events = append(blocks[i].Events, transactionEvents...)
				// We use `models.NewMultiBlockEvents`, as the `transactionEvents`
				// are coming from different Flow blocks.
				evmEvents = models.NewMultiBlockEvents(blocks[i])
				if evmEvents.Err == nil {
					return evmEvents, nil
				}
			}

			currentHeight = blocks[i].Height + 1
		}
	}
	return evmEvents, nil
}

// fetchMissingData is used as a backup mechanism for fetching EVM-related
// events, when the event streaming API returns an inconsistent response.
// An inconsistent response could be an EVM block that references EVM
// transactions which are not present in the response. It falls back
// to using grpc requests instead of streaming.
func (r *RPCBlockTrackingSubscriber) fetchMissingData(
	ctx context.Context,
	blockEvents flow.BlockEvents,
) models.BlockEvents {
	// remove existing events
	blockEvents.Events = nil

	for _, eventType := range blocksFilter(r.chain).EventTypes {
		recoveredEvents, err := r.client.GetEventsForHeightRange(
			ctx,
			eventType,
			blockEvents.Height,
			blockEvents.Height,
		)
		if err != nil {
			return models.NewBlockEventsError(err)
		}

		if len(recoveredEvents) != 1 {
			return models.NewBlockEventsError(
				fmt.Errorf(
					"received %d but expected 1 event for height %d",
					len(recoveredEvents),
					blockEvents.Height,
				),
			)
		}

		blockEvents.Events = append(blockEvents.Events, recoveredEvents[0].Events...)
	}

	return models.NewSingleBlockEvents(blockEvents)
}

// accumulateEventsMissingBlock will keep receiving transaction events until it can produce a valid
// EVM block event containing a block and transactions. At that point it will reset the recovery mode
// and return the valid block events.
func (r *RPCBlockTrackingSubscriber) accumulateEventsMissingBlock(events flow.BlockEvents) models.BlockEvents {
	txEvents := events.Events
	// Sort `EVM.TransactionExecuted` events
	sort.Slice(txEvents, func(i, j int) bool {
		if txEvents[i].TransactionIndex != txEvents[j].TransactionIndex {
			return txEvents[i].TransactionIndex < txEvents[j].TransactionIndex
		}
		return txEvents[i].EventIndex < txEvents[j].EventIndex
	})
	r.recoveredEvents = append(r.recoveredEvents, txEvents...)
	events.Events = r.recoveredEvents

	// We use `models.NewMultiBlockEvents`, as the `transactionEvents`
	// are coming from different Flow blocks.
	recovered := models.NewMultiBlockEvents(events)
	r.recovery = recovered.Err != nil

	if !r.recovery {
		r.recoveredEvents = nil
	}

	return recovered
}

// recover tries to recover from an invalid data sent over the event stream.
//
// An invalid data can be a cause of corrupted index or network issue from the source,
// in which case we might miss one of the events (missing transaction), or it can be
// due to a failure from the system transaction which commits an EVM block, which results
// in missing EVM block event but present transactions.
func (r *RPCBlockTrackingSubscriber) recover(
	ctx context.Context,
	events flow.BlockEvents,
	err error,
) models.BlockEvents {
	r.logger.Warn().Err(err).Msgf(
		"failed to parse EVM block events for Flow height: %d, entering recovery",
		events.Height,
	)

	if errors.Is(err, errs.ErrMissingBlock) || r.recovery {
		return r.accumulateEventsMissingBlock(events)
	}

	if errors.Is(err, errs.ErrMissingTransactions) {
		return r.fetchMissingData(ctx, events)
	}

	return models.NewBlockEventsError(err)
}
