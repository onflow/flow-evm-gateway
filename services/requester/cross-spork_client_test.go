package requester

import (
	"testing"

	"github.com/stretchr/testify/require"

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
	t.Run("add and validate", func(t *testing.T) {
		clients := &sporkClients{}

		client1 := testutils.SetupClientForRange(10, 100)
		client2 := testutils.SetupClientForRange(101, 200)

		require.NoError(t, clients.add(client2))
		require.NoError(t, clients.add(client1))

		require.True(t, clients.continuous())

		require.Equal(t, client1, clients.get(10))
		require.Equal(t, client1, clients.get(100))
		require.Equal(t, client1, clients.get(20))
		require.Equal(t, client2, clients.get(120))
		require.Equal(t, client2, clients.get(101))
		require.Equal(t, client2, clients.get(200))

		require.Equal(t, nil, clients.get(5))
		require.Equal(t, nil, clients.get(300))
	})

	t.Run("add and validate not-continues", func(t *testing.T) {
		clients := &sporkClients{}

		client1 := testutils.SetupClientForRange(10, 30)
		client2 := testutils.SetupClientForRange(50, 80)

		require.NoError(t, clients.add(client1))
		require.NoError(t, clients.add(client2))

		require.False(t, clients.continuous())
	})
}
