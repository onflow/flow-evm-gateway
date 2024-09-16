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
		owner := uint64Bytes(1337)
		key := uint64Bytes(32)

		count := 100
		for i := 0; i < count; i++ {
			h := uint64(i)
			reg := NewRegister(db, h)
			val := uint64Bytes(h)

			err := reg.SetValue(owner, key, val)
			require.NoError(t, err)
		}

		for i := 0; i < count; i++ {
			h := uint64(i)
			reg := NewRegister(db, h)
			val := uint64Bytes(h)

			retVal, err := reg.GetValue(owner, key)
			require.NoError(t, err)

			require.Equal(t, val, retVal)
		}

	})

	runDB("multiple sets for different accounts and get at latest height", t, func(t *testing.T, db *Storage) {

		height1 := uint64(1)
		owner11 := uint64Bytes(101)
		key11 := uint64Bytes(500)
		val11 := uint64Bytes(1001)
		key15 := uint64Bytes(500)
		val15 := uint64Bytes(2002)

		owner21 := uint64Bytes(105)
		key21 := uint64Bytes(600)
		val21 := uint64Bytes(1002)
		key22 := uint64Bytes(500)
		val22 := uint64Bytes(2002)

		reg := NewRegister(db, height1)
		err := reg.SetValue(owner11, key11, val11)
		require.NoError(t, err)

		height2 := uint64(3)
		reg = NewRegister(db, height2)
		err = reg.SetValue(owner21, key21, val21)
		require.NoError(t, err)
		err = reg.SetValue(owner21, key22, val22)
		require.NoError(t, err)

		height3 := uint64(5)
		reg = NewRegister(db, height3)
		err = reg.SetValue(owner11, key15, val15)
		require.NoError(t, err)
		err = reg.SetValue(owner21, key22, val22)
		require.NoError(t, err)

		reg = NewRegister(db, uint64(0))
		// not found
		val, err := reg.GetValue(owner11, key11)
		require.Nil(t, err)
		require.Nil(t, val)

		val, err = reg.GetValue(owner21, key21)
		require.NoError(t, err)
		require.Nil(t, val)

		reg = NewRegister(db, uint64(1))
		val, err = reg.GetValue(owner11, key11)
		require.NoError(t, err)
		require.Equal(t, val11, val)

		reg = NewRegister(db, uint64(2))
		val, err = reg.GetValue(owner11, key11)
		require.NoError(t, err)
		require.Equal(t, val11, val)

		reg = NewRegister(db, uint64(3))
		val, err = reg.GetValue(owner11, key11)
		require.NoError(t, err)
		require.Equal(t, val11, val)

		reg = NewRegister(db, uint64(5))
		val, err = reg.GetValue(owner11, key15)
		require.NoError(t, err)
		require.Equal(t, val15, val)

		// todo write more examples

	})

}
