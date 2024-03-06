package models

import (
	"context"
	"fmt"
	"github.com/onflow/flow-evm-gateway/models/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func Test_RestartableEngine(t *testing.T) {
	t.Parallel()

	t.Run("shouldn't restart if nil is returned", func(t *testing.T) {
		t.Parallel()
		mockEngine := mocks.NewEngine(t)
		mockEngine.
			On("Run", mock.Anything).
			Return(func(ctx context.Context) error {
				return nil
			}).
			Once()

		r := NewRestartableEngine(mockEngine, 5, zerolog.New(zerolog.NewTestWriter(t)))
		err := r.Run(context.Background())
		require.NoError(t, err)
	})

	t.Run("shouldn't restart if non-recoverable error is returned", func(t *testing.T) {
		t.Parallel()
		retErr := fmt.Errorf("non-receoverable error")
		mockEngine := mocks.NewEngine(t)
		mockEngine.
			On("Run", mock.Anything).
			Return(func(ctx context.Context) error {
				return retErr
			}).
			Once()

		r := NewRestartableEngine(mockEngine, 5, zerolog.New(zerolog.NewTestWriter(t)))
		err := r.Run(context.Background())
		require.EqualError(t, retErr, err.Error())
	})

	t.Run("should restart when recoverable error is returned and then return the error after retries", func(t *testing.T) {
		t.Parallel()
		retries := uint(5)
		prevTime := time.Now()
		prevDiff := time.Duration(0)

		mockEngine := mocks.NewEngine(t)
		mockEngine.
			On("Run", mock.Anything).
			Return(func(ctx context.Context) error {
				curDiff := time.Now().Sub(prevTime)
				// make sure time diff increases with each retry
				assert.True(t, prevDiff < curDiff)
				prevDiff = curDiff
				return ErrDisconnected
			}).
			Times(int(retries))

		r := NewRestartableEngine(mockEngine, retries, zerolog.New(zerolog.NewTestWriter(t)))
		err := r.Run(context.Background())
		require.EqualError(t, ErrDisconnected, err.Error())
	})

	t.Run("should restart when recoverable error is returned but then return nil after error is no longer returned", func(t *testing.T) {
		t.Parallel()
		mockEngine := mocks.NewEngine(t)
		mockEngine.
			On("Run", mock.Anything).
			Return(func(ctx context.Context) error {
				mockEngine.
					On("Run", mock.Anything).
					Return(func(ctx context.Context) error {
						return nil
					}).
					Once()

				return ErrDisconnected
			}).
			Once()

		r := NewRestartableEngine(mockEngine, 5, zerolog.New(zerolog.NewTestWriter(t)))
		err := r.Run(context.Background())
		require.NoError(t, err)
	})
}
