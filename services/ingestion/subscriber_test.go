package ingestion

import (
	"context"
	"testing"

	"github.com/onflow/flow-go-sdk/access"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/services/testutils"

	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
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
