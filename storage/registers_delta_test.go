package storage_test

import (
	"fmt"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	flowGo "github.com/onflow/flow-go/model/flow"

	"github.com/onflow/flow-evm-gateway/storage"
	pebbleStorage "github.com/onflow/flow-evm-gateway/storage/pebble"
)

func Test_RegisterDeltaWithStorage(t *testing.T) {
	owner := []byte{0x01}
	ownerAddress := flowGo.BytesToAddress(owner)
	owner2 := []byte{0x02}
	key := []byte{0x03}
	value1 := []byte{0x05}
	value2 := []byte{0x06}

	runDB("get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Len(t, v, 0)
	})

	runDB("set register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)
	})

	runDB("set-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)
	})

	runDB("set-set-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = r.SetValue(owner, key, value2)
		require.NoError(t, err)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value2, v)
	})

	runDB("set-unset-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = r.SetValue(owner, key, nil)
		require.NoError(t, err)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		// not actually nil, but empty
		require.Len(t, v, 0)
	})

	runDB("set-next-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = commit(t, db, r, s)
		require.NoError(t, err)

		r.Reset(1)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)
	})

	runDB("set-dont-commit-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		r.Reset(1)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)
	})

	runDB("set-next-set-next-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = commit(t, db, r, s)
		require.NoError(t, err)

		r.Reset(1)

		err = r.SetValue(owner, key, value2)
		require.NoError(t, err)

		err = commit(t, db, r, s)
		require.NoError(t, err)

		r.Reset(2)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value2, v)
	})

	runDB("set-next-unset-next-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = commit(t, db, r, s)
		require.NoError(t, err)

		r.Reset(1)

		err = r.SetValue(owner, key, nil)
		require.NoError(t, err)

		err = commit(t, db, r, s)
		require.NoError(t, err)

		r.Reset(2)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		// not actually nil, but empty
		require.Len(t, v, 0)
	})

	runDB("get with wrong owner", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(1, s)

		_, err := r.GetValue(owner2, key)
		require.Error(t, err)
	})

	runDB("commit with wrong owner", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisters(db, ownerAddress)
		r := storage.NewRegistersDelta(0, s)

		err := r.SetValue(owner2, key, value1)
		require.NoError(t, err)

		err = commit(t, db, r, s)
		require.Error(t, err)
	})

	runDB("cache db values", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := &mockRegisterIndex{
			callback: func(id flowGo.RegisterID, height uint64) (flowGo.RegisterValue, error) {
				return value1, nil
			},
		}
		r := storage.NewRegistersDelta(1, s)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)

		v, err = r.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)

		// only one call to the index
		require.Equal(t, uint(1), s.callCount)
	})

	runDB("cache nil db values", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := &mockRegisterIndex{
			callback: func(id flowGo.RegisterID, height uint64) (flowGo.RegisterValue, error) {
				return nil, nil
			},
		}
		r := storage.NewRegistersDelta(1, s)

		v, err := r.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)

		v, err = r.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)

		// only one call to the index
		require.Equal(t, uint(1), s.callCount)
	})

	runDB("dont cache err", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := &mockRegisterIndex{
			callback: func(id flowGo.RegisterID, height uint64) (flowGo.RegisterValue, error) {
				return nil, fmt.Errorf("error")
			},
		}
		r := storage.NewRegistersDelta(1, s)

		_, err := r.GetValue(owner, key)
		require.Error(t, err)

		_, err = r.GetValue(owner, key)
		require.Error(t, err)

		require.Equal(t, uint(2), s.callCount)
	})
}

type mockRegisterIndex struct {
	callCount uint
	callback  func(id flowGo.RegisterID, height uint64) (flowGo.RegisterValue, error)
}

func (m *mockRegisterIndex) Get(id flowGo.RegisterID, height uint64) (flowGo.RegisterValue, error) {
	m.callCount++
	return m.callback(id, height)
}

func runDB(name string, t *testing.T, f func(t *testing.T, db *pebbleStorage.Storage)) {
	dir := t.TempDir()

	db, err := pebbleStorage.New(dir, zerolog.New(zerolog.NewTestWriter(t)))
	require.NoError(t, err)

	t.Run(name, func(t *testing.T) {
		f(t, db)
	})
}

// commit is an example on how to commit the delta to storage.
func commit(
	t *testing.T,
	db *pebbleStorage.Storage,
	d *storage.RegistersDelta,
	r *pebbleStorage.RegisterIndex,
) error {
	batch := db.NewBatch()

	err := r.Store(d.GetUpdates(), 0, batch)

	if err != nil {
		return err
	}

	err = batch.Commit(pebble.Sync)
	require.NoError(t, err)
	return nil
}
