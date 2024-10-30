package ingestion

import (
	"context"
	"errors"
	"fmt"

	"github.com/onflow/cadence/common"
	"github.com/onflow/flow-go/fvm/evm/events"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
)

type EventSubscriber interface {
	// Subscribe to EVM events from the provided height, and return a chanel with the events.
	//
	// The BlockEvents type will contain an optional error in case
	// the error happens, the consumer of the chanel should handle it.
	Subscribe(ctx context.Context) <-chan models.BlockEvents
}

var _ EventSubscriber = &RPCEventSubscriber{}

type RPCEventSubscriber struct {
	logger zerolog.Logger

	client *requester.CrossSporkClient
	chain  flowGo.ChainID
	height uint64

	recovery        bool
	recoveredEvents []flow.Event
}

func NewRPCEventSubscriber(
	logger zerolog.Logger,
	client *requester.CrossSporkClient,
	chainID flowGo.ChainID,
	startHeight uint64,
) *RPCEventSubscriber {
	logger = logger.With().Str("component", "subscriber").Logger()
	return &RPCEventSubscriber{
		logger: logger,

		client: client,
		chain:  chainID,
		height: startHeight,
	}
}

// Subscribe will retrieve all the events from the provided height. If the height is from previous
// sporks, it will first backfill all the events in all the previous sporks, and then continue
// to listen all new events in the current spork.
//
// If error is encountered during backfill the subscription will end and the response chanel will be closed.
func (r *RPCEventSubscriber) Subscribe(ctx context.Context) <-chan models.BlockEvents {
	eventsChan := make(chan models.BlockEvents)

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
func (r *RPCEventSubscriber) subscribe(ctx context.Context, height uint64) <-chan models.BlockEvents {
	eventsChan := make(chan models.BlockEvents)

	_, err := r.client.GetBlockHeaderByHeight(ctx, height)
	if err != nil {
		err = fmt.Errorf("failed to subscribe for events, the block height %d doesn't exist: %w", height, err)
		eventsChan <- models.NewBlockEventsError(err)
		return eventsChan
	}

	// we always use heartbeat interval of 1 to have the least amount of delay from the access node
	eventStream, errChan, err := r.client.SubscribeEventsByBlockHeight(
		ctx,
		height,
		blocksFilter(r.chain),
		access.WithHeartbeatInterval(1),
	)
	if err != nil {
		eventsChan <- models.NewBlockEventsError(
			fmt.Errorf("failed to subscribe to events by block height: %d, with: %w", height, err),
		)
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

			case blockEvents, ok := <-eventStream:
				if !ok {
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewBlockEventsError(err)
					return
				}

				evmEvents := models.NewBlockEvents(blockEvents)
				// if events contain an error, or we are in a recovery mode
				if evmEvents.Err != nil || r.recovery {
					evmEvents = r.recover(ctx, blockEvents, evmEvents.Err)
					// if we are still in recovery go to the next event
					if r.recovery {
						continue
					}
				}

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

				eventsChan <- models.NewBlockEventsError(fmt.Errorf("%w: %w", errs.ErrDisconnected, err))
				return
			}
		}
	}()

	return eventsChan
}

// backfill will use the provided height and with the client for the provided spork will start backfilling
// events. Before subscribing, it will check what is the latest block in the current spork (defined by height)
// and check for each event it receives whether we reached the end, if we reach the end it will increase
// the height by one (next height), and check if we are still in previous sporks, if so repeat everything,
// otherwise return.
func (r *RPCEventSubscriber) backfill(ctx context.Context, height uint64) <-chan models.BlockEvents {
	eventsChan := make(chan models.BlockEvents)

	go func() {
		defer func() {
			close(eventsChan)
		}()

		for {
			// check if the current height is still in past sporks, and if not return since we are done with backfilling
			if !r.client.IsPastSpork(height) {
				r.logger.Info().
					Uint64("height", height).
					Msg("completed backfilling")

				return
			}

			latestHeight, err := r.client.GetLatestHeightForSpork(ctx, height)
			if err != nil {
				eventsChan <- models.NewBlockEventsError(err)
				return
			}

			r.logger.Info().
				Uint64("start-height", height).
				Uint64("last-spork-height", latestHeight).
				Msg("backfilling spork")

			for ev := range r.subscribe(ctx, height) {
				eventsChan <- ev

				if ev.Err != nil {
					return
				}

				r.logger.Debug().Msg(fmt.Sprintf("backfilling [%d / %d]...", ev.Events.CadenceHeight(), latestHeight))

				if ev.Events != nil && ev.Events.CadenceHeight() == latestHeight {
					height = ev.Events.CadenceHeight() + 1 // go to next height in the next spork

					r.logger.Info().
						Uint64("next-height", height).
						Msg("reached the end of spork, checking next spork")

					break
				}
			}
		}
	}()

	return eventsChan
}

// fetchMissingData is used as a backup mechanism for fetching EVM-related
// events, when the event streaming API returns an inconsistent response.
// An inconsistent response could be an EVM block that references EVM
// transactions which are not present in the response. It falls back
// to using grpc requests instead of streaming.
func (r *RPCEventSubscriber) fetchMissingData(
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

	return models.NewBlockEvents(blockEvents)
}

// accumulateEventsMissingBlock will keep receiving transaction events until it can produce a valid
// EVM block event containing a block and transactions. At that point it will reset the recovery mode
// and return the valid block events.
func (r *RPCEventSubscriber) accumulateEventsMissingBlock(events flow.BlockEvents) models.BlockEvents {
	r.recoveredEvents = append(r.recoveredEvents, events.Events...)
	events.Events = r.recoveredEvents

	recovered := models.NewBlockEvents(events)
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
func (r *RPCEventSubscriber) recover(
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

// blockFilter define events we subscribe to:
// A.{evm}.EVM.BlockExecuted and A.{evm}.EVM.TransactionExecuted,
// where {evm} is EVM deployed contract address, which depends on the chain ID we configure.
func blocksFilter(chainId flowGo.ChainID) flow.EventFilter {
	evmAddress := common.Address(systemcontracts.SystemContractsForChain(chainId).EVMContract.Address)

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

	return flow.EventFilter{
		EventTypes: []string{
			blockExecutedEvent,
			transactionExecutedEvent,
		},
	}
}
