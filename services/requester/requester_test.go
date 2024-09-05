package requester

import (
	"fmt"
	"testing"

	"github.com/onflow/cadence"
	"github.com/stretchr/testify/require"
)

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
