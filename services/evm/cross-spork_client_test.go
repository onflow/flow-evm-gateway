package evm

import (
	"context"
	"testing"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/testutils"
)

func Test_CrossSporkClient(t *testing.T) {
	t.Run("contains", func(t *testing.T) {
		first := uint64(10)
		last := uint64(100)
		client := sporkClient{
			firstHeight: first,
			lastHeight:  last,
			client:      nil,
		}

		require.True(t, client.contains(first+1))
		require.True(t, client.contains(last-1))
		require.True(t, client.contains(first))
		require.True(t, client.contains(last))
		require.False(t, client.contains(2))
		require.False(t, client.contains(200))
	})
}

func Test_CrossSporkClients(t *testing.T) {
	logger := zerolog.Nop()
	t.Run("add and validate", func(t *testing.T) {
		clients := &sporkClients{}

		client1 := testutils.SetupClientForRange(10, 100)
		client2 := testutils.SetupClientForRange(101, 200)
		client3 := testutils.SetupClientForRange(201, 300)

		require.NoError(t, clients.add(logger, client2))
		require.NoError(t, clients.add(logger, client3))
		require.NoError(t, clients.add(logger, client1))

		require.True(t, clients.continuous())

		require.Equal(t, client1, clients.get(10))
		require.Equal(t, client1, clients.get(100))
		require.Equal(t, client1, clients.get(20))
		require.Equal(t, client2, clients.get(120))
		require.Equal(t, client2, clients.get(101))
		require.Equal(t, client2, clients.get(200))

		require.Equal(t, nil, clients.get(5))
		require.Equal(t, nil, clients.get(310))
	})

	t.Run("add and validate not-continuous", func(t *testing.T) {
		clients := &sporkClients{}

		client1 := testutils.SetupClientForRange(10, 30)
		client2 := testutils.SetupClientForRange(50, 80)

		require.NoError(t, clients.add(logger, client1))
		require.NoError(t, clients.add(logger, client2))

		require.False(t, clients.continuous())
	})
}

func Test_CrossSpork(t *testing.T) {
	t.Run("client", func(t *testing.T) {
		past1Last := uint64(300)
		past2Last := uint64(500)
		currentLast := uint64(1000)
		current := testutils.SetupClientForRange(501, currentLast)
		past1 := testutils.SetupClientForRange(100, past1Last)
		past2 := testutils.SetupClientForRange(301, past2Last)

		client, err := NewCrossSporkClient(
			current,
			[]access.Client{past2, past1},
			zerolog.Nop(),
			flowGo.Previewnet,
		)
		require.NoError(t, err)

		c, err := client.GetClientForHeight(150)
		require.NoError(t, err)
		require.Equal(t, past1, c)

		c, err = client.GetClientForHeight(past2Last - 1)
		require.NoError(t, err)
		require.Equal(t, past2, c)

		c, err = client.GetClientForHeight(600)
		require.NoError(t, err)
		require.Equal(t, current, c)

		c, err = client.GetClientForHeight(10)
		require.Nil(t, c)
		require.ErrorIs(t, err, errs.ErrHeightOutOfRange)

		require.True(t, client.IsPastSpork(200))
		require.True(t, client.IsPastSpork(past1Last))
		require.False(t, client.IsPastSpork(past2Last+1))
		require.False(t, client.IsPastSpork(600))

		_, err = client.ExecuteScriptAtBlockHeight(context.Background(), 20, []byte{}, nil)
		require.ErrorIs(t, err, errs.ErrHeightOutOfRange)

		_, err = client.GetBlockHeaderByHeight(context.Background(), 20)
		require.ErrorIs(t, err, errs.ErrHeightOutOfRange)

		_, _, err = client.SubscribeEventsByBlockHeight(context.Background(), 20, flow.EventFilter{}, nil)
		require.ErrorIs(t, err, errs.ErrHeightOutOfRange)

		height, err := client.GetLatestHeightForSpork(context.Background(), past2Last-10)
		require.NoError(t, err)
		require.Equal(t, past2Last, height)

		height, err = client.GetLatestHeightForSpork(context.Background(), past1Last-10)
		require.NoError(t, err)
		require.Equal(t, past1Last, height)

		height, err = client.GetLatestHeightForSpork(context.Background(), currentLast-10)
		require.NoError(t, err)
		require.Equal(t, currentLast, height)

		_, err = client.GetLatestHeightForSpork(context.Background(), 10)
		require.ErrorIs(t, err, errs.ErrHeightOutOfRange)

		require.ErrorContains(t, err, "invalid height not in available range: 10")
	})
}
