package api

import (
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

type MockFilter struct {
	expiredStatus bool
}

func (mf *MockFilter) id() rpc.ID {
	return "1"
}

func (mf *MockFilter) expired() bool {
	return mf.expiredStatus
}

func (mf *MockFilter) prepare() {}

func (mf *MockFilter) updateUsed(last uint64) {
	// update activities if needed
}

func (mf *MockFilter) next() uint64 {
	return 0
}

func TestFilterExpiryChecker(t *testing.T) {
	var testCases = []struct {
		name  string
		setup func(api *PullAPI)
	}{
		{
			name:  "NoFilters",
			setup: func(api *PullAPI) {},
		},
		{
			name: "FilterNotExpired",
			setup: func(api *PullAPI) {
				api.filters["not_expired"] = &MockFilter{
					expiredStatus: false,
				}
			},
		},
		{
			name: "OnlyExpiredFilters",
			setup: func(api *PullAPI) {
				api.filters["expired"] = &MockFilter{
					expiredStatus: true,
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			api := &PullAPI{
				filters: make(map[rpc.ID]filter),
				config:  &config.Config{FilterExpiry: time.Millisecond * 5},
			}

			tc.setup(api)
			go api.filterExpiryChecker()

			// sleep to let the goroutine work
			time.Sleep(100 * time.Millisecond)

			if len(api.filters) > 0 {
				for id, f := range api.filters {
					require.Falsef(t, f.expired(), "filter with id %s should be expired", id)
				}
			}
		})
	}
}
