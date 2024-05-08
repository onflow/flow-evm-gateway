package ingestion

import (
	"context"
	"testing"

	"github.com/onflow/flow-evm-gateway/models"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/mocks"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupClient(client *mocks.Client, startHeight uint64, endHeight uint64) {
	client.
		On("GetLatestBlockHeader", mock.Anything, mock.Anything).
		Return(&flow.BlockHeader{
			Height: endHeight,
		}, nil)

	client.
		On("GetBlockHeaderByHeight", mock.Anything, mock.Anything).
		Return(func(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
			if height < startHeight || height > endHeight {
				return nil, storage.ErrNotFound
			}

			return &flow.BlockHeader{
				Height: height,
			}, nil
		})

	client.
		On("SubscribeEventsByBlockHeight", mock.Anything, mock.Anything, mock.Anything).
		Return(func() (<-chan flowGo.BlockEvents, <-chan error, error) {
			events := make(chan flowGo.BlockEvents)

			for i := startHeight; i <= endHeight; i++ {
				events <- flowGo.BlockEvents{
					BlockHeight: i,
				}
			}

			return events, make(chan error), nil
		})
}

// this test simulates two previous sporks and current spork
// the subscriber should start with spork1Client then proceed to
// spork2Client and end with currentClient.
// All event heights should be emitted in sequence.
func Test_Subscribing(t *testing.T) {

	currentClient := &mocks.Client{}
	spork1Client := &mocks.Client{}
	spork2Client := &mocks.Client{}

	const endHeight = 50
	setupClient(spork1Client, 1, 10)
	setupClient(spork2Client, 11, 20)
	setupClient(currentClient, 21, endHeight)

	client, err := models.NewCrossSporkClient(currentClient, zerolog.Nop())
	require.NoError(t, err)

	err = client.AddSpork(spork2Client)
	require.NoError(t, err)

	err = client.AddSpork(spork1Client)
	require.NoError(t, err)

	subscriber := NewRPCSubscriber(client, flowGo.Emulator, zerolog.Nop())

	events := subscriber.Subscribe(context.Background(), 1)

	var prevHeight uint64

	for ev := range events {
		require.NoError(t, ev.Err)

		// this makes sure all the event heights are sequential
		eventHeight := ev.Events.CadenceHeight()
		require.Equal(t, prevHeight+1, eventHeight)
		prevHeight = eventHeight
	}

	// this makes sure we indexed all the events
	require.Equal(t, endHeight, prevHeight)
}
