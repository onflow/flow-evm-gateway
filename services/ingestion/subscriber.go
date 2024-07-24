package ingestion

import (
	"context"
	"errors"
	"fmt"

	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go/fvm/evm/events"

	"github.com/onflow/flow-evm-gateway/models"
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
	Subscribe(ctx context.Context, height uint64) <-chan models.BlockEvents
}

var _ EventSubscriber = &RPCSubscriber{}

type RPCSubscriber struct {
	client            *requester.CrossSporkClient
	chain             flowGo.ChainID
	heartbeatInterval uint64
	logger            zerolog.Logger
}

func NewRPCSubscriber(
	client *requester.CrossSporkClient,
	heartbeatInterval uint64,
	chainID flowGo.ChainID,
	logger zerolog.Logger,
) *RPCSubscriber {
	logger = logger.With().Str("component", "subscriber").Logger()
	return &RPCSubscriber{
		client:            client,
		heartbeatInterval: heartbeatInterval,
		chain:             chainID,
		logger:            logger,
	}
}

// Subscribe will retrieve all the events from the provided height. If the height is from previous
// sporks, it will first backfill all the events in all the previous sporks, and then continue
// to listen all new events in the current spork.
//
// If error is encountered during backfill the subscription will end and the response chanel will be closed.
func (r *RPCSubscriber) Subscribe(ctx context.Context, height uint64) <-chan models.BlockEvents {
	events := make(chan models.BlockEvents)

	go func() {
		defer func() {
			close(events)
		}()

		// if the height is from the previous spork, backfill all the events from previous sporks first
		if r.client.IsPastSpork(height) {
			r.logger.Info().
				Uint64("height", height).
				Msg("height found in previous spork, starting to backfill")

			// backfill all the missed events, handling of context cancellation is done by the producer
			for ev := range r.backfill(ctx, height) {
				events <- ev

				if ev.Err != nil {
					return
				}

				// keep updating height, so after we are done back-filling
				// it will be at the first height in the current spork
				height = ev.Events.CadenceHeight()
			}

			// after back-filling is done, increment height by one,
			// so we start with the height in the current spork
			height = height + 1
		}

		r.logger.Info().
			Uint64("next-height", height).
			Msg("backfilling done, subscribe for live data")

		// subscribe in the current spork, handling of context cancellation is done by the producer
		for ev := range r.subscribe(ctx, height, access.WithHeartbeatInterval(r.heartbeatInterval)) {
			events <- ev
		}

		r.logger.Warn().Msg("ended subscription for events")
	}()

	return events
}

// subscribe to events by the provided height and handle any errors.
//
// Subscribing to EVM specific events and handle any disconnection errors
// as well as context cancellations.
func (r *RPCSubscriber) subscribe(ctx context.Context, height uint64, opts ...access.SubscribeOption) <-chan models.BlockEvents {
	events := make(chan models.BlockEvents)

	_, err := r.client.GetBlockHeaderByHeight(ctx, height)
	if err != nil {
		err = fmt.Errorf("failed to subscribe for events, the block height %d doesn't exist: %w", height, err)
		events <- models.NewBlockEventsError(err)
		return events
	}

	evs, errs, err := r.client.SubscribeEventsByBlockHeight(ctx, height, r.blocksFilter(), opts...)
	if err != nil {
		events <- models.NewBlockEventsError(fmt.Errorf("failed to subscribe to events by block height: %w", err))
		return events
	}

	go func() {
		defer func() {
			close(events)
		}()

		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				r.logger.Info().Msg("event ingestion received done signal")
				return

			case blockEvents, ok := <-evs:
				if !ok {
					var err error
					err = models.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					events <- models.NewBlockEventsError(err)
					return
				}

				events <- models.NewBlockEvents(blockEvents)

			case err, ok := <-errs:
				if !ok {
					var err error
					err = models.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					events <- models.NewBlockEventsError(err)
					return
				}

				events <- models.NewBlockEventsError(errors.Join(err, models.ErrDisconnected))
				return
			}
		}
	}()

	return events
}

// backfill will use the provided height and with the client for the provided spork will start backfilling
// events. Before subscribing, it will check what is the latest block in the current spork (defined by height)
// and check for each event it receives whether we reached the end, if we reach the end it will increase
// the height by one (next height), and check if we are still in previous sporks, if so repeat everything,
// otherwise return.
func (r *RPCSubscriber) backfill(ctx context.Context, height uint64) <-chan models.BlockEvents {
	events := make(chan models.BlockEvents)

	go func() {
		defer func() {
			close(events)
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
				events <- models.NewBlockEventsError(err)
				return
			}

			r.logger.Info().
				Uint64("start-height", height).
				Uint64("last-spork-height", latestHeight).
				Msg("backfilling spork")

			for ev := range r.subscribe(ctx, height, access.WithHeartbeatInterval(1)) {
				events <- ev

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

	return events
}

// blockFilter define events we subscribe to:
// A.{evm}.EVM.BlockExecuted and A.{evm}.EVM.TransactionExecuted,
// where {evm} is EVM deployed contract address, which depends on the chain ID we configure.
func (r *RPCSubscriber) blocksFilter() flow.EventFilter {
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

	return flow.EventFilter{
		EventTypes: []string{
			blockExecutedEvent,
			transactionExecutedEvent,
		},
	}
}
