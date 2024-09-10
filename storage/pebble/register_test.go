package pebble

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_RegisterSetGet(t *testing.T) {

	runDB("set and get at same height", t, func(t *testing.T, db *Storage) {
		height := uint64(1)
		reg := NewRegister(db, height)

		owner := uint64Bytes(1337)
		key := uint64Bytes(32)
		val := uint64Bytes(124932)

		err := reg.SetValue(owner, key, val)
		require.NoError(t, err)

		retVal, err := reg.GetValue(owner, key)
		require.NoError(t, err)

		require.Equal(t, val, retVal)
	})

	runDB("set and get at latest height", t, func(t *testing.T, db *Storage) {

	})

	runDB("multiple sets for different accounts and get at latest height", t, func(t *testing.T, db *Storage) {

	})

}
