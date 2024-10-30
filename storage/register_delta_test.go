package storage_test

import (
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

	// helper to create a new register delta
	delta := func(t *testing.T, r *pebbleStorage.RegisterStorage, evmBlockHeight uint64) *storage.RegisterDelta {
		ss, err := r.GetSnapshotAt(0)
		require.NoError(t, err)
		return storage.NewRegisterDelta(ss)
	}

	runDB("get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		require.Len(t, v, 0)
	})

	runDB("set register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)
	})

	runDB("set-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)
	})

	runDB("set-set-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = d.SetValue(owner, key, value2)
		require.NoError(t, err)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value2, v)
	})

	runDB("set-unset-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = d.SetValue(owner, key, nil)
		require.NoError(t, err)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		// not actually nil, but empty
		require.Len(t, v, 0)
	})

	runDB("set-next-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = commit(t, db, d, r)
		require.NoError(t, err)

		d = delta(t, r, 1)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value1, v)
	})

	runDB("set-dont-commit-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		d = delta(t, r, 1)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)
	})

	runDB("set-next-set-next-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = commit(t, db, d, r)
		require.NoError(t, err)

		d = delta(t, r, 1)

		err = d.SetValue(owner, key, value2)
		require.NoError(t, err)

		err = commit(t, db, d, r)
		require.NoError(t, err)

		d = delta(t, r, 2)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value2, v)
	})

	runDB("set-next-unset-next-get register", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		err := d.SetValue(owner, key, value1)
		require.NoError(t, err)

		err = commit(t, db, d, r)
		require.NoError(t, err)

		d = delta(t, r, 1)

		err = d.SetValue(owner, key, nil)
		require.NoError(t, err)

		err = commit(t, db, d, r)
		require.NoError(t, err)

		d = delta(t, r, 2)

		v, err := d.GetValue(owner, key)
		require.NoError(t, err)
		// not actually nil, but empty
		require.Len(t, v, 0)
	})

	runDB("get with wrong owner", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		r := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, r, 0)

		_, err := d.GetValue(owner2, key)
		require.Error(t, err)
	})

	runDB("commit with wrong owner", t, func(t *testing.T, db *pebbleStorage.Storage) {
		t.Parallel()

		s := pebbleStorage.NewRegisterStorage(db, ownerAddress)
		d := delta(t, s, 0)

		err := d.SetValue(owner2, key, value1)
		require.NoError(t, err)

		err = commit(t, db, d, s)
		require.Error(t, err)
	})
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
	d *storage.RegisterDelta,
	r *pebbleStorage.RegisterStorage,
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
