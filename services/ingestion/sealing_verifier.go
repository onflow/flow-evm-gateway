package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/engine/access/rpc/backend/events"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"go.uber.org/atomic"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

var _ models.Engine = (*SealingVerifier)(nil)

// SealingVerifier verifies that soft finality events received over the Access polling API match the
// actually sealed results from the event stream.
type SealingVerifier struct {
	*models.EngineStatus

	logger zerolog.Logger
	client *requester.CrossSporkClient
	chain  flowGo.ChainID

	startHeight uint64
	eventsHash  *pebble.EventsHash

	// unsealedBlocksToVerify contains the events has for unsealed blocks by the ingestion engine
	// Cache the unsealed data until the sealed data is available to verify.
	unsealedBlocksToVerify map[uint64]flow.Identifier

	// sealedBlocksToVerify contains the events hash for sealed blocks return by the Access node
	// Note: we also track sealed blocks since it's possible for the sealed data stream to get ahead
	// of the unsealed data ingestion. In this case, we need to cache the sealed data until the unsealed
	// data is available.
	sealedBlocksToVerify map[uint64]flow.Identifier

	lastUnsealedHeight *atomic.Uint64
	lastSealedHeight   *atomic.Uint64

	mu sync.Mutex
}

// NewSealingVerifier creates a new sealing verifier.
func NewSealingVerifier(
	logger zerolog.Logger,
	client *requester.CrossSporkClient,
	chain flowGo.ChainID,
	eventsHash *pebble.EventsHash,
	startHeight uint64,
) *SealingVerifier {
	// startHeight is the first height to verify, which is one block after the last processed height
	lastProcessedUnsealedHeight := startHeight
	if lastProcessedUnsealedHeight > 0 {
		lastProcessedUnsealedHeight--
	}

	return &SealingVerifier{
		EngineStatus:           models.NewEngineStatus(),
		logger:                 logger.With().Str("component", "sealing_verifier").Logger(),
		client:                 client,
		chain:                  chain,
		startHeight:            startHeight,
		eventsHash:             eventsHash,
		unsealedBlocksToVerify: make(map[uint64]flow.Identifier),
		sealedBlocksToVerify:   make(map[uint64]flow.Identifier),
		lastUnsealedHeight:     atomic.NewUint64(lastProcessedUnsealedHeight),
		lastSealedHeight:       atomic.NewUint64(0),
	}
}

// Stop the engine.
func (v *SealingVerifier) Stop() {
	v.MarkDone()
	<-v.Stopped()
}

// SetStartHeight sets the start height for the sealing verifier.
// This is used to update the height when backfilling to skip verification of already sealed blocks.
func (v *SealingVerifier) SetStartHeight(height uint64) {
	v.startHeight = height
}

// AddFinalizedBlock adds events for an unsealed block to the sealing verifier for verification when
// the sealed data is received.
func (v *SealingVerifier) AddFinalizedBlock(events flow.BlockEvents) error {
	return v.onUnsealedEvents(events)
}

// Run executes the sealing verifier.
// This method will block until the context is canceled or an error occurs.
func (v *SealingVerifier) Run(ctx context.Context) error {
	defer v.MarkStopped()

	lastVerifiedHeight, err := v.eventsHash.ProcessedSealedHeight()
	if err != nil {
		if !errors.Is(err, errs.ErrStorageNotInitialized) {
			return fmt.Errorf("failed to get processed sealed height: %w", err)
		}

		// lastVerifiedHeight should be the block before the startHeight
		// handle the case where startHeight is 0 like when running with the emulator
		if lastVerifiedHeight = v.startHeight; lastVerifiedHeight > 0 {
			lastVerifiedHeight = v.startHeight - 1
		}
		if err := v.eventsHash.SetProcessedSealedHeight(lastVerifiedHeight); err != nil {
			return fmt.Errorf("failed to initialize processed sealed height: %w", err)
		}
	}
	v.lastSealedHeight.Store(lastVerifiedHeight)
	startHeight := v.lastSealedHeight.Load() + 1

	if v.client.IsPastSpork(startHeight) {
		if err := v.backfill(ctx, startHeight); err != nil {
			return fmt.Errorf("failed to backfill: %w", err)
		}
		startHeight = v.lastSealedHeight.Load() + 1
	}

	var eventsChan <-chan flow.BlockEvents
	var errChan <-chan error

	subscriptionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	reconnectHeight := startHeight
	connect := func(height uint64) error {
		var err error
		for {
			eventsChan, errChan, err = v.client.SubscribeEventsByBlockHeight(
				subscriptionCtx,
				height,
				blocksFilter(v.chain),
				access.WithHeartbeatInterval(1),
			)

			if err != nil {
				// access node has not sealed the next height yet, wait and try again
				// this typically happens when the AN reboots and the stream is reconnected before
				// it has sealed the next block
				if strings.Contains(err.Error(), "could not get start height") &&
					strings.Contains(err.Error(), "higher than highest indexed height") {
					v.logger.Info().Err(err).Uint64("height", height).Msg("waiting for start block to be sealed")
					time.Sleep(time.Second)
					continue
				}
				return err
			}

			return nil
		}
	}

	v.logger.Info().
		Uint64("start_sealed_height", startHeight).
		Uint64("start_unsealed_height", v.lastUnsealedHeight.Load()+1).
		Msg("starting verifier")

	if err := connect(reconnectHeight); err != nil {
		return fmt.Errorf("failed to subscribe for finalized block events on height: %d, with: %w", reconnectHeight, err)
	}

	v.MarkReady()
	for {
		select {
		case <-ctx.Done():
			v.logger.Info().Msg("received done signal")
			return nil

		case <-v.Done():
			v.logger.Info().Msg("received stop signal")
			cancel()
			return nil

		case sealedEvents, ok := <-eventsChan:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("failed to receive block events: %w", err)
			}

			if err := v.onSealedEvents(sealedEvents); err != nil {
				return fmt.Errorf("failed to process sealed events: %w", err)
			}

			reconnectHeight = sealedEvents.Height + 1

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

			if err := connect(reconnectHeight); err != nil {
				return fmt.Errorf("failed to resubscribe for finalized block headers on height: %d, with: %w", reconnectHeight, err)
			}
		}
	}
}

// backfill fetches EVM events for blocks before the current spork's root block, and adds them to the
// verifier's sealed events cache.
func (v *SealingVerifier) backfill(ctx context.Context, height uint64) error {
	eventTypes := blocksFilter(v.chain).EventTypes

	sporkRootHeight := v.client.CurrentSporkRootHeight()

	v.logger.Info().
		Uint64("start_height", height).
		Uint64("end_height", sporkRootHeight-1).
		Msg("backfilling verifier")

	startHeight := height
	for {
		endHeight := startHeight + events.DefaultMaxHeightRange
		if endHeight >= sporkRootHeight {
			endHeight = sporkRootHeight - 1
		}

		// this will fail if the start and end heights are now in the same spork. The only way that
		// could happen is if we skipped a spork. That's unlikely to happen, so not handling the case
		// for now.
		blockEvents, err := v.client.GetEventsForHeightRange(ctx, eventTypes[0], startHeight, endHeight)
		if err != nil {
			return fmt.Errorf("failed to get events for height range %d-%d: %w", startHeight, endHeight, err)
		}

		txEvents, err := v.client.GetEventsForHeightRange(ctx, eventTypes[1], startHeight, endHeight)
		if err != nil {
			return fmt.Errorf("failed to get events for height range %d-%d: %w", startHeight, endHeight, err)
		}

		// it's technically possible that the end height is modified by the Access node to match the
		// sealed height. Since this is backfill mode, the AN is a historic AN so it is not sealing
		// any new blocks. Therefore, both requests must have the same number of events.
		if len(blockEvents) != len(txEvents) {
			return fmt.Errorf("received unexpected number of events for height range %d-%d: %d != %d", startHeight, endHeight, len(blockEvents), len(txEvents))
		}

		var currentBlockTxEvents []flow.Event
		for i, blockEvent := range blockEvents {
			currentBlockTxEvents = append(currentBlockTxEvents, txEvents[i].Events...)

			// if the system transaction failed, there won't be an EVM block event, but there may
			// be EVM transactions. Group all transactions into the next block.
			if len(blockEvent.Events) != 1 {
				v.logger.Warn().
					Uint64("height", blockEvent.Height).
					Str("block_id", blockEvent.BlockID.String()).
					Msg("missing evm block event. will accumulate transactions into the next block")
				continue
			}

			blockEvent.Events = append(blockEvent.Events, currentBlockTxEvents...)
			if err := v.onSealedEvents(blockEvent); err != nil {
				return fmt.Errorf("failed to verify block events for height %d: %w", blockEvent.Height, err)
			}

			// transactions sucessessfully grouped with a block. reset the list
			currentBlockTxEvents = nil
		}

		startHeight = endHeight + 1
		if startHeight >= sporkRootHeight {
			return nil
		}
	}
}

// onSealedEvents processes sealed events
// if unsealed events are found for the same height, the events are verified.
// otherwise, the sealed events are cached for future verification.
func (v *SealingVerifier) onSealedEvents(sealedEvents flow.BlockEvents) error {
	// Note: there should be an unsealed event entry, even for blocks with no transactions

	// ensure we have received sealed data for all blocks
	if sealedEvents.Height > 0 && !v.lastSealedHeight.CompareAndSwap(sealedEvents.Height-1, sealedEvents.Height) {
		// note: this conditional skips updating the lastSealedHeight if the height is 0. this is
		// desired since it will be the last height when we process block 1.
		return fmt.Errorf("received sealed events out of order: expected %d, got %d", v.lastSealedHeight.Load()+1, sealedEvents.Height)
	}

	sealedHash, err := CalculateHash(sealedEvents)
	if err != nil {
		return fmt.Errorf("failed to calculate hash for sealed events for height %d: %w", sealedEvents.Height, err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	unsealedHash, err := v.getUnsealedEventsHash(sealedEvents.Height)

	// cache the sealed hash if
	// 1. we haven't processed the unsealed data for this block yet
	// 2. we have the data, but the state was rolled back to a previous height. In this case, wait
	//    until we've reprocessed data for the height.
	if errors.Is(err, errs.ErrEntityNotFound) || sealedEvents.Height > v.lastUnsealedHeight.Load() {
		// we haven't processed the unsealed data for this block yet, cache the sealed hash
		v.sealedBlocksToVerify[sealedEvents.Height] = sealedHash
		return nil
	}
	if err != nil {
		return fmt.Errorf("no unsealed events found for height %d: %w", sealedEvents.Height, err)
	}

	if err := v.verifyBlock(sealedEvents.Height, sealedHash, unsealedHash); err != nil {
		v.logger.Fatal().Err(err).
			Uint64("height", sealedEvents.Height).
			Str("block_id", sealedEvents.BlockID.String()).
			Msg("failed to verify block events")
		return fmt.Errorf("failed to verify block events for %d: %w", sealedEvents.Height, err)
	}

	v.logger.Info().
		Uint64("height", sealedEvents.Height).
		Int("num_events", len(sealedEvents.Events)).
		Msg("verified height from sealed events")

	return nil
}

// onUnsealedEvents processes unsealed events.
// if sealed events are found for the same height, the events are verified.
// otherwise, the unsealed events are cached for future verification.
func (v *SealingVerifier) onUnsealedEvents(unsealedEvents flow.BlockEvents) error {
	unsealedHash, err := CalculateHash(unsealedEvents)
	if err != nil {
		return fmt.Errorf("failed to calculate hash for block %d: %w", unsealedEvents.Height, err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// note: do this inside the lock to avoid a race with onSealedEvents
	if err := v.eventsHash.Store(unsealedEvents.Height, unsealedHash); err != nil {
		return fmt.Errorf("failed to store events hash for block %d: %w", unsealedEvents.Height, err)
	}

	// update the last unsealed height after successfully storing the hash
	if unsealedEvents.Height > 0 && !v.lastUnsealedHeight.CompareAndSwap(unsealedEvents.Height-1, unsealedEvents.Height) {
		// note: this conditional skips updating the lastUnsealedHeight if the height is 0. this is
		// desired since it will be the last height when we process block 1.
		return fmt.Errorf("received unsealed events out of order: expected %d, got %d", v.lastUnsealedHeight.Load()+1, unsealedEvents.Height)
	}

	sealedHash, ok := v.sealedBlocksToVerify[unsealedEvents.Height]
	if !ok {
		v.unsealedBlocksToVerify[unsealedEvents.Height] = unsealedHash
		return nil
	}

	if err := v.verifyBlock(unsealedEvents.Height, sealedHash, unsealedHash); err != nil {
		v.logger.Fatal().Err(err).
			Uint64("height", unsealedEvents.Height).
			Str("block_id", unsealedEvents.BlockID.String()).
			Msg("failed to verify block events")
		return fmt.Errorf("failed to verify block events for %d: %w", unsealedEvents.Height, err)
	}

	v.logger.Info().
		Uint64("height", unsealedEvents.Height).
		Int("num_events", len(unsealedEvents.Events)).
		Msg("verified height from unsealed events")

	return nil
}

// getUnsealedEventsHash returns the events hash for the given height without taking a lock
func (v *SealingVerifier) getUnsealedEventsHash(height uint64) (flow.Identifier, error) {
	if hash, ok := v.unsealedBlocksToVerify[height]; ok {
		return hash, nil
	}

	hash, err := v.eventsHash.GetByHeight(height)
	if err != nil {
		return flow.Identifier{}, fmt.Errorf("failed to get events hash for block %d: %w", height, err)
	}

	// note: don't cache it here since we will usually not revisit this height

	return hash, nil
}

// verifyBlock verifies that the hash of the sealed events matches the hash of unsealed events stored
// for the same height.
func (v *SealingVerifier) verifyBlock(height uint64, sealedHash, unsealedHash flow.Identifier) error {
	// always delete since we will crash on error anyway
	defer delete(v.unsealedBlocksToVerify, height)
	defer delete(v.sealedBlocksToVerify, height)

	if sealedHash != unsealedHash {
		return fmt.Errorf("event hash mismatch: expected %s, got %s", sealedHash, unsealedHash)
	}

	if err := v.eventsHash.SetProcessedSealedHeight(height); err != nil {
		return fmt.Errorf("failed to store processed sealed height: %w", err)
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

	// need canonical order before hashing
	sort.Slice(flowEvents, func(i, j int) bool {
		if flowEvents[i].TransactionIndex != flowEvents[j].TransactionIndex {
			return flowEvents[i].TransactionIndex < flowEvents[j].TransactionIndex
		}
		return flowEvents[i].EventIndex < flowEvents[j].EventIndex
	})

	return flowGo.BlockEvents{
		BlockID:        blockID,
		BlockHeight:    events.Height,
		BlockTimestamp: events.BlockTimestamp,
		Events:         flowEvents,
	}, nil
}
