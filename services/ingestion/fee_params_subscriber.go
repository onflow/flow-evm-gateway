package ingestion

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/onflow/cadence/common"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
)

type FeeParamsSubscriber interface {
	Subscribe(ctx context.Context) <-chan *models.FeeParamsEvents
}

var _ FeeParamsSubscriber = &FeeParamsEventSubscriber{}

type FeeParamsEventSubscriber struct {
	logger zerolog.Logger
	client *requester.CrossSporkClient
	chain  flowGo.ChainID
	height uint64
}

func NewFeeParamsEventSubscriber(
	logger zerolog.Logger,
	client *requester.CrossSporkClient,
	chainID flowGo.ChainID,
	startHeight uint64,
) *FeeParamsEventSubscriber {
	logger = logger.With().Str("component", "fee_params_subscriber").Logger()
	return &FeeParamsEventSubscriber{
		logger: logger,
		client: client,
		chain:  chainID,
		height: startHeight,
	}
}

func (r *FeeParamsEventSubscriber) Subscribe(ctx context.Context) <-chan *models.FeeParamsEvents {
	// buffered channel so that the decoding of the events can happen in parallel to other operations
	eventsChan := make(chan *models.FeeParamsEvents, 1000)

	go func() {
		defer func() {
			close(eventsChan)
		}()

		for evt := range r.subscribe(ctx, r.height) {
			eventsChan <- evt
		}

		r.logger.Warn().Msg("ended subscription for fee parameters changed events")
	}()

	return eventsChan
}

// subscribe to events by the provided height and handle any errors.
//
// Subscribing to EVM specific events and handle any disconnection errors
// as well as context cancellations.
func (r *FeeParamsEventSubscriber) subscribe(ctx context.Context, height uint64) <-chan *models.FeeParamsEvents {
	// create the channel with a buffer size of 1,
	// to avoid blocking on the two error cases below
	eventsChan := make(chan *models.FeeParamsEvents, 1)

	_, err := r.client.GetBlockHeaderByHeight(ctx, height)
	if err != nil {
		err = fmt.Errorf("failed to subscribe for FeeParametersChanged events, the block height %d doesn't exist: %w", height, err)
		eventsChan <- models.NewFeeParamsEventsError(err)
		close(eventsChan)
		return eventsChan
	}

	var blockEventsStream <-chan flow.BlockEvents
	var errChan <-chan error

	lastReceivedHeight := height
	connect := func(height uint64) error {
		var err error

		// we always use heartbeat interval of 1 to have the
		// least amount of delay from the access node
		blockEventsStream, errChan, err = r.client.SubscribeEventsByBlockHeight(
			ctx,
			height,
			feeParamsEventFilter(r.chain),
			access.WithHeartbeatInterval(1),
		)

		return err
	}

	if err := connect(lastReceivedHeight); err != nil {
		eventsChan <- models.NewFeeParamsEventsError(
			fmt.Errorf("failed to subscribe for FeeParametersChanged events by block height: %d, with: %w", height, err),
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

			case blockEvents, ok := <-blockEventsStream:
				if !ok {
					// typically we receive an error in the errChan before the channels are closed
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewFeeParamsEventsError(err)
					return
				}

				lastReceivedHeight = blockEvents.Height
				if len(blockEvents.Events) == 0 {
					continue
				}
				feeParamsEvents := models.NewFeeParamsEvents(blockEvents)
				eventsChan <- feeParamsEvents

			case err, ok := <-errChan:
				if !ok {
					// typically we receive an error in the errChan before the channels are closed
					var err error
					err = errs.ErrDisconnected
					if ctx.Err() != nil {
						err = ctx.Err()
					}
					eventsChan <- models.NewFeeParamsEventsError(err)
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
					eventsChan <- models.NewFeeParamsEventsError(fmt.Errorf("%w: %w", errs.ErrDisconnected, err))
					return
				}

				if err := connect(lastReceivedHeight + 1); err != nil {
					eventsChan <- models.NewFeeParamsEventsError(
						fmt.Errorf(
							"failed to resubscribe for events on height: %d, with: %w",
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

// feeParamsEventFilter defines the FlowFees set of events we subscribe to:
// - A.{flow_fees}.FlowFees.FeeParametersChanged,
// where {flow_fees} is the FlowFees deployed contract address for the
// configured chain ID.
func feeParamsEventFilter(chainID flowGo.ChainID) flow.EventFilter {
	contracts := systemcontracts.SystemContractsForChain(chainID)
	flowFeesAddress := common.Address(contracts.FlowFees.Address)

	feeParametersChangedEvent := common.NewAddressLocation(
		nil,
		flowFeesAddress,
		models.FeeParametersChangedQualifiedIdentifier,
	).ID()

	return flow.EventFilter{
		EventTypes: []string{
			feeParametersChangedEvent,
		},
	}
}
