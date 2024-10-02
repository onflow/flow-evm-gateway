package models

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSequentialHeight(t *testing.T) {
	h := NewSequentialHeight(5)
	require.NoError(t, h.Increment(6))
	require.NoError(t, h.Increment(7))
	require.NoError(t, h.Increment(7)) // idempotent

	// invalid
	require.ErrorIs(t, h.Increment(9), ErrInvalidHeight)
	require.ErrorIs(t, h.Increment(5), ErrInvalidHeight)
	require.ErrorIs(t, h.Increment(math.MaxUint64), ErrInvalidHeight)
}
