package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetryInterceptor(t *testing.T) {
	expecterErr := status.Error(codes.ResourceExhausted, "resource exhausted")
	interceptor := retryInterceptor(100*time.Millisecond, 10*time.Millisecond)

	testCases := []struct {
		name           string
		invoker        func(callCount int) error
		maxRequestTime time.Duration
		callCount      int // expect exact count
		minCallCount   int // min, for when using a timeout
		expectedErr    error
	}{
		{
			name: "no error",
			invoker: func(callCount int) error {
				return nil
			},
			maxRequestTime: 10 * time.Millisecond,
			callCount:      1,
			expectedErr:    nil,
		},
		{
			name: "succeeds on 3rd attempt",
			invoker: func(callCount int) error {
				if callCount >= 3 {
					return nil
				}
				return expecterErr
			},
			maxRequestTime: 40 * time.Millisecond,
			callCount:      3,
			expectedErr:    nil,
		},
		{
			name: "fails after timeout",
			invoker: func(callCount int) error {
				return expecterErr
			},
			maxRequestTime: 150 * time.Millisecond, // add a buffer for test slowness
			minCallCount:   10,
			expectedErr:    expecterErr,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			callCount := 0
			invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				callCount++
				return tc.invoker(callCount)
			}

			start := time.Now()
			err := interceptor(
				context.Background(), "", nil, nil, nil,
				invoker,
			)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}

			if tc.minCallCount > 0 {
				assert.GreaterOrEqual(t, callCount, tc.minCallCount)
			} else {
				assert.Equal(t, callCount, tc.callCount)
			}
			assert.LessOrEqual(t, time.Since(start), tc.maxRequestTime)
		})
	}
}
