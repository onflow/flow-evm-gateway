package pebble

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Register(t *testing.T) {
	owner := []byte{0x01}
	key := []byte{0x03}
	value1 := []byte{0x05}
	value2 := []byte{0x06}

	runDB("get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Len(t, v, 0)
	})

	runDB("set register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)
	})

	runDB("set-get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)
	})

	runDB("set-set-get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = r.SetValue(owner, key, value2)
		require.NoError(t, err)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value2, v)
	})

	runDB("set-unset-get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = r.SetValue(owner, key, nil)
		require.NoError(t, err)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		// not actually nil, but empty
		require.Len(t, v, 0)
	})

	runDB("set-next-get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		r = NewRegister(db, 1, nil)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)
	})

	runDB("set-next-set-next-get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		r = NewRegister(db, 1, nil)

		err = r.SetValue(owner, key, value2)
		require.NoError(t, err)

		r = NewRegister(db, 2, nil)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value2, v)
	})

	runDB("set-next-unset-next-get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegister(db, 0, nil)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		r = NewRegister(db, 1, nil)

		err = r.SetValue(owner, key, nil)
		require.NoError(t, err)

		r = NewRegister(db, 2, nil)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		// not actually nil, but empty
		require.Len(t, v, 0)
	})
}
