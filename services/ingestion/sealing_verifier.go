package ingestion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

var _ models.Engine = &SealingVerifier{}

// SealingVerifier verifies that soft finality events received over the Access polling API match the
// actually sealed results from the event stream.
type SealingVerifier struct {
	*models.EngineStatus

	logger zerolog.Logger

	client *requester.CrossSporkClient
	chain  flowGo.ChainID

	eventsHash     *pebble.EventsHash
	blocksToVerify map[uint64]flow.Identifier

	mu sync.Mutex
}

// NewSealingVerifier creates a new sealing verifier.
func NewSealingVerifier(
	logger zerolog.Logger,
	client *requester.CrossSporkClient,
	chain flowGo.ChainID,
	eventsHash *pebble.EventsHash,
) *SealingVerifier {
	return &SealingVerifier{
		EngineStatus: models.NewEngineStatus(),
		logger:       logger,
		client:       client,
		chain:        chain,
		eventsHash:   eventsHash,
	}
}

// Stop the engine.
func (v *SealingVerifier) Stop() {
	v.MarkDone()
	<-v.Stopped()
}

// AddBlock adds a block to the sealing verifier for verification when the sealed data is received.
func (v *SealingVerifier) AddBlock(events flow.BlockEvents) error {
	hash, err := CalculateHash(events)
	if err != nil {
		return fmt.Errorf("failed to calculate hash for block %d: %w", events.Height, err)
	}

	if err := v.eventsHash.Store(events.Height, hash); err != nil {
		return fmt.Errorf("failed to store events hash for block %d: %w", events.Height, err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.blocksToVerify[events.Height] = hash

	return nil
}

// Run executes the sealing verifier.
// This method will block until the context is canceled or an error occurs.
func (v *SealingVerifier) Run(ctx context.Context) error {
	defer v.MarkStopped()

	lastVerifiedHeight, err := v.eventsHash.ProcessedSealedHeight()
	if err != nil {
		return fmt.Errorf("failed to get processed sealed height: %w", err)
	}

	var eventsChan <-chan flow.BlockEvents
	var errChan <-chan error

	subscriptionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lastReceivedHeight := lastVerifiedHeight + 1
	connect := func(height uint64) error {
		var err error
		eventsChan, errChan, err = v.client.SubscribeEventsByBlockHeight(
			subscriptionCtx,
			height,
			blocksFilter(v.chain),
			access.WithHeartbeatInterval(1),
		)
		return err
	}

	if err := connect(lastReceivedHeight); err != nil {
		return fmt.Errorf("failed to subscribe for finalized block events on height: %d, with: %w", lastReceivedHeight, err)
	}

	v.MarkReady()
	for {
		select {
		case <-ctx.Done():
			v.logger.Info().Msg("sealing verifier received done signal")
			return nil

		case <-v.Done():
			v.logger.Info().Msg("sealing verifier received stop signal")
			cancel()
			return nil

		case blockEvents, ok := <-eventsChan:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("failed to receive block events: %w", err)
			}

			if err := v.verifyBlock(blockEvents); err != nil {
				v.logger.Fatal().Err(err).
					Uint64("height", blockEvents.Height).
					Str("block_id", blockEvents.BlockID.String()).
					Msg("failed to verify block events")
				return fmt.Errorf("failed to verify block events for %d: %w", blockEvents.Height, err)
			}

			if err := v.eventsHash.SetProcessedSealedHeight(blockEvents.Height); err != nil {
				return fmt.Errorf("failed to store processed sealed height: %w", err)
			}

		case err, ok := <-errChan:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("failed to receive block events: %w", err)
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
				return fmt.Errorf("%w: %w", errs.ErrDisconnected, err)
			}

			if err := connect(lastReceivedHeight + 1); err != nil {
				return fmt.Errorf("failed to resubscribe for finalized block headers on height: %d, with: %w", lastReceivedHeight+1, err)
			}
		}
	}
}

// getEventsHash returns the events hash for the given height
func (v *SealingVerifier) getEventsHash(height uint64) (flow.Identifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if hash, ok := v.blocksToVerify[height]; ok {
		return hash, nil
	}

	hash, err := v.eventsHash.GetByHeight(height)
	if err != nil {
		return flow.Identifier{}, fmt.Errorf("failed to get events hash for block %d: %w", height, err)
	}

	// note: don't cache it here since we will not revisit this height

	return hash, nil
}

// verifyBlock verifies that the hash of the sealed events matches the hash of unsealed events stored
// for the same height.
func (v *SealingVerifier) verifyBlock(sealedEvents flow.BlockEvents) error {
	unsealedHash, err := v.getEventsHash(sealedEvents.Height)
	if err != nil {
		return fmt.Errorf("no unsealed events found for height %d: %w", sealedEvents.Height, err)
	}

	sealedHash, err := CalculateHash(sealedEvents)
	if err != nil {
		return fmt.Errorf("failed to calculate hash for sealed events: %w", err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// always delete since we will crash on error anyway
	defer delete(v.blocksToVerify, sealedEvents.Height)

	if sealedHash != unsealedHash {
		return fmt.Errorf("event hash mismatch: expected %s, got %s", sealedHash, unsealedHash)
	}

	return nil
}

// CalculateHash calculates the hash of the given block events object.
func CalculateHash(events flow.BlockEvents) (flow.Identifier, error) {
	// convert to strip cadence payload objects
	converted, err := convertFlowBlockEvents(events)
	if err != nil {
		return flow.Identifier{}, err
	}

	hash := flowGo.MakeID(converted)
	return flow.BytesToID(hash[:]), nil
}

// convertFlowBlockEvents converts a flow.BlockEvents (flow-go-sdk) to a flowGo.BlockEvents (flow-go).
func convertFlowBlockEvents(events flow.BlockEvents) (flowGo.BlockEvents, error) {
	blockID, err := flowGo.ByteSliceToId(events.BlockID.Bytes())
	if err != nil {
		return flowGo.BlockEvents{}, fmt.Errorf("failed to convert block ID: %w", err)
	}

	flowEvents := make([]flowGo.Event, len(events.Events))
	for i, e := range events.Events {
		txID, err := flowGo.ByteSliceToId(e.TransactionID.Bytes())
		if err != nil {
			return flowGo.BlockEvents{}, fmt.Errorf("failed to convert transaction ID %s: %w", e.TransactionID.Hex(), err)
		}
		flowEvents[i] = flowGo.Event{
			Type:             flowGo.EventType(e.Type),
			TransactionID:    txID,
			TransactionIndex: uint32(e.TransactionIndex),
			EventIndex:       uint32(e.EventIndex),
			Payload:          e.Payload,
		}
	}

	return flowGo.BlockEvents{
		BlockID:        blockID,
		BlockHeight:    events.Height,
		BlockTimestamp: events.BlockTimestamp,
		Events:         flowEvents,
	}, nil
}
