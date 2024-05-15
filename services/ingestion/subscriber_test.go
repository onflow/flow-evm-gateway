package ingestion

import (
	"context"
	"testing"

	"github.com/onflow/flow-go-sdk/access"

	"github.com/onflow/flow-evm-gateway/models"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/mocks"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type mockClient struct {
	*mocks.Client
	getLatestBlockHeaderFunc         func(context.Context, bool) (*flow.BlockHeader, error)
	getBlockHeaderByHeightFunc       func(context.Context, uint64) (*flow.BlockHeader, error)
	subscribeEventsByBlockHeightFunc func(context.Context, uint64, flow.EventFilter, ...access.SubscribeOption) (<-chan flow.BlockEvents, <-chan error, error)
}

func (c *mockClient) GetBlockHeaderByHeight(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
	return c.getBlockHeaderByHeightFunc(ctx, height)
}

func (c *mockClient) GetLatestBlockHeader(ctx context.Context, sealed bool) (*flow.BlockHeader, error) {
	return c.getLatestBlockHeaderFunc(ctx, sealed)
}

func (c *mockClient) SubscribeEventsByBlockHeight(
	ctx context.Context,
	startHeight uint64,
	filter flow.EventFilter,
	opts ...access.SubscribeOption,
) (<-chan flow.BlockEvents, <-chan error, error) {
	return c.subscribeEventsByBlockHeightFunc(ctx, startHeight, filter, opts...)
}

func setupClient(startHeight uint64, endHeight uint64) access.Client {
	return &mockClient{
		Client: &mocks.Client{},
		getLatestBlockHeaderFunc: func(ctx context.Context, sealed bool) (*flow.BlockHeader, error) {
			return &flow.BlockHeader{
				Height: endHeight,
			}, nil
		},
		getBlockHeaderByHeightFunc: func(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
			if height < startHeight || height > endHeight {
				return nil, storage.ErrNotFound
			}

			return &flow.BlockHeader{
				Height: height,
			}, nil
		},
		subscribeEventsByBlockHeightFunc: func(
			ctx context.Context,
			startHeight uint64,
			filter flow.EventFilter,
			opts ...access.SubscribeOption,
		) (<-chan flow.BlockEvents, <-chan error, error) {
			events := make(chan flow.BlockEvents)

			go func() {
				defer close(events)

				for i := startHeight; i <= endHeight; i++ {
					events <- flow.BlockEvents{
						Height: i,
					}
				}
			}()

			return events, make(chan error), nil
		},
	}
}

// this test simulates two previous sporks and current spork
// the subscriber should start with spork1Client then proceed to
// spork2Client and end with currentClient.
// All event heights should be emitted in sequence.
func Test_Subscribing(t *testing.T) {

	const endHeight = 50
	sporkClients := []access.Client{
		setupClient(1, 10),
		setupClient(11, 20),
	}
	currentClient := setupClient(21, endHeight)

	client, err := models.NewCrossSporkClient(currentClient, sporkClients, zerolog.Nop())
	require.NoError(t, err)

	subscriber := NewRPCSubscriber(client, flowGo.Emulator, zerolog.Nop())

	events := subscriber.Subscribe(context.Background(), 1)

	var prevHeight uint64

	for ev := range events {
		if prevHeight == endHeight {
			require.ErrorIs(t, ev.Err, models.ErrDisconnected)
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
