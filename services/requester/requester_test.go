package requester

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk/access/mocks"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/config"
)

func Test_Caching(t *testing.T) {
	t.Run("Get balance at height cached", func(t *testing.T) {
		mockClient := &mocks.Client{}

		cache := expirable.NewLRU[string, cadence.Value](1000, nil, time.Second)
		e := createEVM(t, cache, mockClient)

		height := uint64(100)
		address, _ := cadence.NewString("123")
		balance := cadence.NewInt(1)

		mockClient.
			On("ExecuteScriptAtBlockHeight", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(balance, nil).
			Once()

		require.Equal(t, 0, cache.Len()) // empty cache

		// first request goes through the above mock client,
		// additional requests should be processed with cache, note the above mock client
		// is only set to once, so if cache is a miss it would fail to call the client again
		for i := 0; i < 5; i++ {
			val, err := e.executeScriptAtHeight(context.Background(), getBalance, height, []cadence.Value{address})
			require.NoError(t, err)
			require.Equal(t, balance, val)
			// cache should be filled
			require.Equal(t, 1, cache.Len())
		}
	})

	t.Run("Get balance at latest height cached", func(t *testing.T) {
		mockClient := &mocks.Client{}

		cache := expirable.NewLRU[string, cadence.Value](1000, nil, time.Second)
		e := createEVM(t, cache, mockClient)

		height := LatestBlockHeight
		address, _ := cadence.NewString("123")
		balance := cadence.NewInt(1)

		mockClient.
			On("ExecuteScriptAtLatestBlock", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(balance, nil).
			Once()

		require.Equal(t, 0, cache.Len()) // empty cache

		// first request goes through the above mock client,
		// additional requests should be processed with cache, note the above mock client
		// is only set to once, so if cache is a miss it would fail to call the client again
		for i := 0; i < 5; i++ {
			val, err := e.executeScriptAtHeight(context.Background(), getBalance, height, []cadence.Value{address})
			require.NoError(t, err)
			require.Equal(t, balance, val)
			// cache should be filled
			require.Equal(t, 1, cache.Len())
		}
	})

	t.Run("Get balance cache expires and is added again", func(t *testing.T) {
		mockClient := &mocks.Client{}

		cacheExpiry := time.Millisecond * 100
		cache := expirable.NewLRU[string, cadence.Value](1000, nil, cacheExpiry)
		e := createEVM(t, cache, mockClient)

		height := LatestBlockHeight
		address, _ := cadence.NewString("123")
		balance := cadence.NewInt(1)

		mockClient.
			On("ExecuteScriptAtLatestBlock", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(balance, nil).
			Once()

		require.Equal(t, 0, cache.Len()) // empty cache

		// first request goes through the above mock client,
		// additional requests should be processed with cache, note the above mock client
		// is only set to once, so if cache is a miss it would fail to call the client again
		for i := 0; i < 5; i++ {
			val, err := e.executeScriptAtHeight(context.Background(), getBalance, height, []cadence.Value{address})
			require.NoError(t, err)
			require.Equal(t, balance, val)
			// cache should be filled
			require.Equal(t, 1, cache.Len())
		}

		// wait for cache expiry
		time.Sleep(cacheExpiry + 100*time.Millisecond)

		require.Equal(t, 0, cache.Len()) // make sure cache is empty

		// re-set the mock
		mockClient.
			On("ExecuteScriptAtLatestBlock", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(balance, nil).
			Once()
		val, err := e.executeScriptAtHeight(context.Background(), getBalance, height, []cadence.Value{address})
		require.NoError(t, err)
		require.Equal(t, balance, val)
		require.Equal(t, 1, cache.Len())
	})

	t.Run("Get balance multiple addresses and heights", func(t *testing.T) {
		mockClient := &mocks.Client{}

		cache := expirable.NewLRU[string, cadence.Value](1000, nil, time.Second)
		e := createEVM(t, cache, mockClient)

		type acc struct {
			height  uint64
			address cadence.String
			balance cadence.Int
		}

		tests := []acc{{
			height:  1002233,
			address: cadence.String("1AC87F33D10b76E8BDd4fb501445A5ec413eb121"),
			balance: cadence.NewInt(23958395),
		}, {
			height:  2002233,
			address: cadence.String("A3014d9F6162a162BAD9Ff15346A4B82A56F841f"),
			balance: cadence.NewInt(1),
		}, {
			height:  3002233,
			address: cadence.String("53e6A4b36a56CB68fe54661416Be2c5b3Ee193c9"),
			balance: cadence.NewInt(4),
		}, {
			height:  4002233,
			address: cadence.String("839fEfa0750798B3A0BD9c925871e3f5027a5d44"),
			balance: cadence.NewInt(3),
		}, {
			height:  7002233,
			address: cadence.String("243a064089cF765E1F270B90913Db31cdDf299F5"),
			balance: cadence.NewInt(5),
		}}

		for i, test := range tests {
			mockClient.
				On("ExecuteScriptAtBlockHeight", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(test.balance, nil).
				Once()

			val, err := e.executeScriptAtHeight(context.Background(), getBalance, test.height, []cadence.Value{test.address})
			require.NoError(t, err)
			require.Equal(t, test.balance, val)
			// cache should be filled
			require.Equal(t, i+1, cache.Len())
		}

		require.Equal(t, len(tests), cache.Len())

		// first request goes through the above mock client,
		// additional requests should be processed with cache, note the above mock client
		// is only set to once, so if cache is a miss it would fail to call the client again
		for _, test := range tests {
			val, err := e.executeScriptAtHeight(context.Background(), getBalance, test.height, []cadence.Value{test.address})
			require.NoError(t, err)
			require.Equal(t, test.balance, val)
			// cache should be filled
			require.Equal(t, len(tests), cache.Len())
		}
	})
}

func Test_CacheKey(t *testing.T) {
	addr, _ := cadence.NewString("0x1")
	h := uint64(100)

	key := cacheKey(getBalance, h, []cadence.Value{addr})
	require.Equal(t, fmt.Sprintf("%d%d%s", getBalance, h, string(addr)), key)

	key = cacheKey(getBalance, LatestBlockHeight, []cadence.Value{addr})
	require.Equal(t, fmt.Sprintf("%d%d%s", getBalance, LatestBlockHeight, string(addr)), key)

	key = cacheKey(getNonce, LatestBlockHeight, []cadence.Value{addr})
	require.Equal(t, fmt.Sprintf("%d%d%s", getNonce, LatestBlockHeight, string(addr)), key)

	key = cacheKey(getNonce, h, []cadence.Value{addr})
	require.Equal(t, fmt.Sprintf("%d%d%s", getNonce, h, string(addr)), key)

	key = cacheKey(getLatest, LatestBlockHeight, nil)
	require.Equal(t, fmt.Sprintf("%d%d", getLatest, LatestBlockHeight), key)

	key = cacheKey(getCode, LatestBlockHeight, nil)
	require.Equal(t, "", key)

	key = cacheKey(getBalance, LatestBlockHeight, []cadence.Value{addr, addr})
	require.Equal(t, "", key)

}

func createEVM(t *testing.T, cache *expirable.LRU[string, cadence.Value], mockClient *mocks.Client) *EVM {
	networkID := flowGo.Emulator
	log := zerolog.New(zerolog.NewTestWriter(t))

	client, err := NewCrossSporkClient(mockClient, nil, log, networkID)
	require.NoError(t, err)

	return &EVM{
		client:      client,
		logger:      log,
		scriptCache: cache,
		config: &config.Config{
			FlowNetworkID: networkID,
		},
	}
}
