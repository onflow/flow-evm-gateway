package ingestion

import (
	"context"
	"testing"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	gethCommon "github.com/onflow/go-ethereum/common"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/services/testutils"

	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// this test simulates two previous sporks and current spork
// the subscriber should start with spork1Client then proceed to
// spork2Client and end with currentClient.
// All event heights should be emitted in sequence.
func Test_Subscribing(t *testing.T) {

	const endHeight = 50
	sporkClients := []access.Client{
		testutils.SetupClientForRange(1, 10),
		testutils.SetupClientForRange(11, 20),
	}
	currentClient := testutils.SetupClientForRange(21, endHeight)

	client, err := requester.NewCrossSporkClient(
		currentClient,
		sporkClients,
		zerolog.Nop(),
		flowGo.Previewnet,
	)
	require.NoError(t, err)

	subscriber := NewRPCSubscriber(client, 100, flowGo.Previewnet, zerolog.Nop())

	events := subscriber.Subscribe(context.Background(), 1)

	var prevHeight uint64

	for ev := range events {
		if prevHeight == endHeight {
			require.ErrorIs(t, ev.Err, errs.ErrDisconnected)
			break
		}

		require.NoError(t, ev.Err)

		// this makes sure all the event heights are sequential
		eventHeight := ev.Events.CadenceHeight()
		require.Equal(t, prevHeight+1, eventHeight)
		prevHeight = eventHeight
	}

	// this makes sure we indexed all the events
	require.Equal(t, uint64(endHeight), prevHeight)
}

func Test_SubscribingWithRetryOnError(t *testing.T) {
	endHeight := uint64(10)
	sporkClients := []access.Client{}
	currentClient := testutils.SetupClientForRange(1, endHeight)

	cadenceHeight := uint64(5)
	txCount := 10
	hashes := make([]gethCommon.Hash, txCount)
	flowEvents := make([]flow.Event, 0)

	// generate txs
	for i := 0; i < txCount; i++ {
		cdcEvent, txEvent, tx, _, err := newTransaction(cadenceHeight)
		require.NoError(t, err)
		hashes[i] = tx.Hash()
		flowEvent := flow.Event{
			Type:  string(txEvent.Etype),
			Value: cdcEvent,
		}
		flowEvents = append(flowEvents, flowEvent)
	}

	evmTxEvents := flow.BlockEvents{
		BlockID:        flow.Identifier{0x1},
		Height:         cadenceHeight,
		BlockTimestamp: time.Now(),
		Events:         flowEvents,
	}

	// generate single block
	cdcEvent, evmBlock, blockEvent, err := newBlock(cadenceHeight, hashes)
	require.NoError(t, err)
	flowEvent := flow.Event{
		Type:  string(blockEvent.Etype),
		Value: cdcEvent,
	}
	evmBlockEvents := flow.BlockEvents{
		BlockID:        flow.Identifier{0x1},
		Height:         cadenceHeight,
		BlockTimestamp: time.Now(),
		Events:         []flow.Event{flowEvent},
	}

	currentClient.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.BlockExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{evmBlockEvents}, nil).Once()

	currentClient.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.TransactionExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{evmTxEvents}, nil).Once()

	currentClient.SubscribeEventsByBlockHeightFunc = func(
		ctx context.Context,
		startHeight uint64,
		filter flow.EventFilter,
		opts ...access.SubscribeOption,
	) (<-chan flow.BlockEvents, <-chan error, error) {
		events := make(chan flow.BlockEvents)
		errors := make(chan error)

		blockEvents := flow.BlockEvents{
			BlockID:        flow.Identifier{0x1},
			Height:         cadenceHeight,
			BlockTimestamp: time.Now(),
			Events:         evmTxEvents.Events,
		}

		// generate single block
		cdcEvent, _, blockEvent, err := newBlock(cadenceHeight, hashes[:txCount-2])
		require.NoError(t, err)
		flowEvent := flow.Event{
			Type:  string(blockEvent.Etype),
			Value: cdcEvent,
		}
		blockEvents.Events = append(blockEvents.Events, flowEvent)

		go func() {
			defer close(events)

			for i := startHeight; i <= endHeight; i++ {
				if i == cadenceHeight {
					events <- blockEvents
				} else {
					events <- flow.BlockEvents{
						Height: i,
					}
				}
			}
		}()

		return events, errors, nil
	}

	client, err := requester.NewCrossSporkClient(
		currentClient,
		sporkClients,
		zerolog.Nop(),
		flowGo.Previewnet,
	)
	require.NoError(t, err)

	subscriber := NewRPCSubscriber(client, 100, flowGo.Previewnet, zerolog.Nop())

	events := subscriber.Subscribe(context.Background(), 1)

	var prevHeight uint64

	for ev := range events {
		if prevHeight == endHeight {
			require.ErrorIs(t, ev.Err, errs.ErrDisconnected)
			break
		}

		require.NoError(t, ev.Err)

		// this makes sure all the event heights are sequential
		eventHeight := ev.Events.CadenceHeight()
		require.Equal(t, prevHeight+1, eventHeight)
		prevHeight = eventHeight

		if eventHeight == cadenceHeight {
			assert.Equal(t, evmBlock, ev.Events.Block())
			for i := 0; i < txCount; i++ {
				tx := ev.Events.Transactions()[i]
				assert.Equal(t, hashes[i], tx.Hash())
			}
		}
	}

	// this makes sure we indexed all the events
	require.Equal(t, uint64(endHeight), prevHeight)
}

func Test_SubscribingWithRetryOnErrorMultipleBlocks(t *testing.T) {
	endHeight := uint64(10)
	sporkClients := []access.Client{}
	currentClient := testutils.SetupClientForRange(1, endHeight)

	cadenceHeight := uint64(5)
	txCount := 10
	hashes := make([]gethCommon.Hash, txCount)
	flowEvents := make([]flow.Event, 0)

	// generate txs
	for i := 0; i < txCount; i++ {
		cdcEvent, txEvent, tx, _, err := newTransaction(cadenceHeight)
		require.NoError(t, err)
		hashes[i] = tx.Hash()
		flowEvent := flow.Event{
			Type:  string(txEvent.Etype),
			Value: cdcEvent,
		}
		flowEvents = append(flowEvents, flowEvent)
	}

	evmTxEvents := flow.BlockEvents{
		BlockID:        flow.Identifier{0x1},
		Height:         cadenceHeight,
		BlockTimestamp: time.Now(),
		Events:         flowEvents,
	}

	// generate single block
	cdcEvent, _, blockEvent, err := newBlock(cadenceHeight, hashes)
	require.NoError(t, err)
	flowEvent := flow.Event{
		Type:  string(blockEvent.Etype),
		Value: cdcEvent,
	}
	evmBlockEvents := flow.BlockEvents{
		BlockID:        flow.Identifier{0x1},
		Height:         cadenceHeight,
		BlockTimestamp: time.Now(),
		Events:         []flow.Event{flowEvent},
	}

	currentClient.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.BlockExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{evmBlockEvents, evmBlockEvents}, nil).Once()

	currentClient.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.TransactionExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{evmTxEvents}, nil).Once()

	currentClient.SubscribeEventsByBlockHeightFunc = func(
		ctx context.Context,
		startHeight uint64,
		filter flow.EventFilter,
		opts ...access.SubscribeOption,
	) (<-chan flow.BlockEvents, <-chan error, error) {
		events := make(chan flow.BlockEvents)
		errors := make(chan error)

		blockEvents := flow.BlockEvents{
			BlockID:        flow.Identifier{0x1},
			Height:         cadenceHeight,
			BlockTimestamp: time.Now(),
			Events:         evmTxEvents.Events,
		}

		// generate single block
		cdcEvent, _, blockEvent, err := newBlock(cadenceHeight, hashes[:txCount-2])
		require.NoError(t, err)
		flowEvent := flow.Event{
			Type:  string(blockEvent.Etype),
			Value: cdcEvent,
		}
		blockEvents.Events = append(blockEvents.Events, flowEvent)

		go func() {
			defer close(events)

			for i := startHeight; i <= endHeight; i++ {
				if i == cadenceHeight {
					events <- blockEvents
				} else {
					events <- flow.BlockEvents{
						Height: i,
					}
				}
			}
		}()

		return events, errors, nil
	}

	client, err := requester.NewCrossSporkClient(
		currentClient,
		sporkClients,
		zerolog.Nop(),
		flowGo.Previewnet,
	)
	require.NoError(t, err)

	subscriber := NewRPCSubscriber(client, 100, flowGo.Previewnet, zerolog.Nop())

	events := subscriber.Subscribe(context.Background(), 1)

	var prevHeight uint64

	for ev := range events {
		if prevHeight == endHeight {
			require.ErrorIs(t, ev.Err, errs.ErrDisconnected)
			break
		}

		if prevHeight+1 == cadenceHeight {
			require.Error(t, ev.Err)
			assert.ErrorContains(
				t,
				ev.Err,
				"received multiple Flow block events for height: 5",
			)
			prevHeight = cadenceHeight
		} else {
			require.NoError(t, ev.Err)
			// this makes sure all the event heights are sequential
			eventHeight := ev.Events.CadenceHeight()
			require.Equal(t, prevHeight+1, eventHeight)
			prevHeight = eventHeight
		}
	}

	require.Equal(t, endHeight, prevHeight)
}

func Test_SubscribingWithRetryOnErrorEmptyBlocks(t *testing.T) {
	endHeight := uint64(10)
	sporkClients := []access.Client{}
	currentClient := testutils.SetupClientForRange(1, endHeight)

	cadenceHeight := uint64(5)
	txCount := 10
	hashes := make([]gethCommon.Hash, txCount)
	flowEvents := make([]flow.Event, 0)

	// generate txs
	for i := 0; i < txCount; i++ {
		cdcEvent, txEvent, tx, _, err := newTransaction(cadenceHeight)
		require.NoError(t, err)
		hashes[i] = tx.Hash()
		flowEvent := flow.Event{
			Type:  string(txEvent.Etype),
			Value: cdcEvent,
		}
		flowEvents = append(flowEvents, flowEvent)
	}

	evmTxEvents := flow.BlockEvents{
		BlockID:        flow.Identifier{0x1},
		Height:         cadenceHeight,
		BlockTimestamp: time.Now(),
		Events:         flowEvents,
	}

	currentClient.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.BlockExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{}, nil).Once()

	currentClient.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.TransactionExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{evmTxEvents}, nil).Once()

	currentClient.SubscribeEventsByBlockHeightFunc = func(
		ctx context.Context,
		startHeight uint64,
		filter flow.EventFilter,
		opts ...access.SubscribeOption,
	) (<-chan flow.BlockEvents, <-chan error, error) {
		events := make(chan flow.BlockEvents)
		errors := make(chan error)

		blockEvents := flow.BlockEvents{
			BlockID:        flow.Identifier{0x1},
			Height:         cadenceHeight,
			BlockTimestamp: time.Now(),
			Events:         evmTxEvents.Events,
		}

		// generate single block
		cdcEvent, _, blockEvent, err := newBlock(cadenceHeight, hashes[:txCount-2])
		require.NoError(t, err)
		flowEvent := flow.Event{
			Type:  string(blockEvent.Etype),
			Value: cdcEvent,
		}
		blockEvents.Events = append(blockEvents.Events, flowEvent)

		go func() {
			defer close(events)

			for i := startHeight; i <= endHeight; i++ {
				if i == cadenceHeight {
					events <- blockEvents
				} else {
					events <- flow.BlockEvents{
						Height: i,
					}
				}
			}
		}()

		return events, errors, nil
	}

	client, err := requester.NewCrossSporkClient(
		currentClient,
		sporkClients,
		zerolog.Nop(),
		flowGo.Previewnet,
	)
	require.NoError(t, err)

	subscriber := NewRPCSubscriber(client, 100, flowGo.Previewnet, zerolog.Nop())

	events := subscriber.Subscribe(context.Background(), 1)

	var prevHeight uint64

	for ev := range events {
		if prevHeight == endHeight {
			require.ErrorIs(t, ev.Err, errs.ErrDisconnected)
			break
		}

		if prevHeight+1 == cadenceHeight {
			require.Error(t, ev.Err)
			assert.ErrorContains(
				t,
				ev.Err,
				"received empty Flow block events for height: 5",
			)
			prevHeight = cadenceHeight
		} else {
			require.NoError(t, ev.Err)
			// this makes sure all the event heights are sequential
			eventHeight := ev.Events.CadenceHeight()
			require.Equal(t, prevHeight+1, eventHeight)
			prevHeight = eventHeight
		}
	}

	require.Equal(t, endHeight, prevHeight)
}
