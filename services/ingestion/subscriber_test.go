package ingestion

import (
	"context"
	"testing"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	gethCommon "github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
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

// Test that back-up fetching of EVM events is triggered when the
// Event Streaming API returns an inconsistent response.
// This scenario tests the happy path, when the back-up fetching of
// EVM events through the gRPC API, returns the correct data.
func Test_SubscribingWithRetryOnError(t *testing.T) {
	endHeight := uint64(10)
	sporkClients := []access.Client{}
	currentClient := testutils.SetupClientForRange(1, endHeight)

	cadenceHeight := uint64(5)
	evmTxEvents, txHashes := generateEvmTxEvents(t, cadenceHeight)
	evmBlock, evmBlockEvents := generateEvmBlock(t, cadenceHeight, txHashes)

	setupClientForBackupEventFetching(
		t,
		currentClient,
		cadenceHeight,
		[]flow.BlockEvents{evmBlockEvents},
		evmTxEvents,
		txHashes,
		endHeight,
	)

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
			for i := 0; i < len(txHashes); i++ {
				tx := ev.Events.Transactions()[i]
				assert.Equal(t, txHashes[i], tx.Hash())
			}
		}
	}

	// this makes sure we indexed all the events
	require.Equal(t, uint64(endHeight), prevHeight)
}

// Test that back-up fetching of EVM events is triggered when the
// Event Streaming API returns an inconsistent response.
// This scenario tests the unhappy path, when the back-up fetching
// of EVM events through the gRPC API, returns duplicate EVM blocks.
func Test_SubscribingWithRetryOnErrorMultipleBlocks(t *testing.T) {
	endHeight := uint64(10)
	sporkClients := []access.Client{}
	currentClient := testutils.SetupClientForRange(1, endHeight)

	cadenceHeight := uint64(5)
	evmTxEvents, txHashes := generateEvmTxEvents(t, cadenceHeight)
	_, evmBlockEvents := generateEvmBlock(t, cadenceHeight, txHashes)

	setupClientForBackupEventFetching(
		t,
		currentClient,
		cadenceHeight,
		[]flow.BlockEvents{evmBlockEvents, evmBlockEvents}, // return the same EVM block twice
		evmTxEvents,
		txHashes,
		endHeight,
	)

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
				"received 2 but expected 1 event for height 5",
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

// Test that back-up fetching of EVM events is triggered when the
// Event Streaming API returns an inconsistent response.
// This scenario tests the unhappy path, when the back-up fetching
// of EVM events through the gRPC API, returns no EVM blocks.
func Test_SubscribingWithRetryOnErrorEmptyBlocks(t *testing.T) {
	endHeight := uint64(10)
	sporkClients := []access.Client{}
	currentClient := testutils.SetupClientForRange(1, endHeight)

	cadenceHeight := uint64(5)
	evmTxEvents, txHashes := generateEvmTxEvents(t, cadenceHeight)

	setupClientForBackupEventFetching(
		t,
		currentClient,
		cadenceHeight,
		[]flow.BlockEvents{},
		evmTxEvents,
		txHashes,
		endHeight,
	)

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
				"received 0 but expected 1 event for height 5",
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

func generateEvmTxEvents(t *testing.T, cadenceHeight uint64) (
	flow.BlockEvents,
	[]gethCommon.Hash,
) {
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

	return flow.BlockEvents{
		BlockID:        flow.Identifier{0x1},
		Height:         cadenceHeight,
		BlockTimestamp: time.Now(),
		Events:         flowEvents,
	}, hashes
}

func generateEvmBlock(
	t *testing.T,
	cadenceHeight uint64,
	txHashes []gethCommon.Hash,
) (*models.Block, flow.BlockEvents) {
	// generate single block
	cdcEvent, evmBlock, blockEvent, err := newBlock(cadenceHeight, txHashes)
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

	return evmBlock, evmBlockEvents
}

func setupClientForBackupEventFetching(
	t *testing.T,
	client *testutils.MockClient,
	cadenceHeight uint64,
	evmBlockEvents []flow.BlockEvents,
	evmTxEvents flow.BlockEvents,
	txHashes []gethCommon.Hash,
	endHeight uint64,
) {
	client.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.BlockExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return(evmBlockEvents, nil).Once()

	client.On(
		"GetEventsForHeightRange",
		mock.AnythingOfType("context.backgroundCtx"),
		"A.b6763b4399a888c8.EVM.TransactionExecuted",
		uint64(cadenceHeight),
		uint64(cadenceHeight),
	).Return([]flow.BlockEvents{evmTxEvents}, nil).Once()

	client.SubscribeEventsByBlockHeightFunc = func(
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
		cdcEvent, _, blockEvent, err := newBlock(cadenceHeight, txHashes[:len(txHashes)-2])
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
}
