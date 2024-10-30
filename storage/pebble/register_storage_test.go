package pebble

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go/model/flow"
	"github.com/stretchr/testify/require"
)

func Test_RegisterIndex(t *testing.T) {
	t.Parallel()
	owner := "0x1"
	ownerAddress := flow.BytesToAddress([]byte(owner))
	owner2 := "0x2"
	key := "0x3"
	value := []byte{0x4}

	runDB("get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisterStorage(db, ownerAddress)

		v, err := r.Get(flow.RegisterID{Owner: owner, Key: key}, 0)
		require.NoError(t, err)
		require.Empty(t, v)
	})

	runDB("get register - owner2", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisterStorage(db, ownerAddress)

		_, err := r.Get(flow.RegisterID{Owner: owner2, Key: key}, 0)
		require.Error(t, err)
	})

	runDB("store registers", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisterStorage(db, ownerAddress)

		batch := db.NewBatch()

		err := r.Store(
			flow.RegisterEntries{
				flow.RegisterEntry{
					Key:   flow.RegisterID{Owner: owner, Key: key},
					Value: value,
				},
			},
			0,
			batch,
		)
		require.NoError(t, err)

		v, err := r.Get(flow.RegisterID{Owner: owner, Key: key}, 0)
		require.NoError(t, err)
		// not commited, so value is still empty
		require.Empty(t, v)

		err = batch.Commit(pebble.Sync)
		require.NoError(t, err)

		v, err = r.Get(flow.RegisterID{Owner: owner, Key: key}, 0)
		require.NoError(t, err)
		require.Equal(t, value, v)

		require.NoError(t, err)
	})

	runDB("store registers - owner2", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisterStorage(db, ownerAddress)

		batch := db.NewBatch()

		err := r.Store(
			flow.RegisterEntries{
				flow.RegisterEntry{
					Key:   flow.RegisterID{Owner: owner2, Key: key},
					Value: value,
				},
			},
			0,
			batch,
		)
		require.Error(t, err)
	})
}

func Test_StorageSnapshot(t *testing.T) {

	t.Parallel()
	owner := []byte("0x1")
	ownerAddress := flow.BytesToAddress(owner)
	key := []byte("0x3")
	value := []byte{0x4}

	runDB("get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisterStorage(db, ownerAddress)
		s, err := r.GetSnapshotAt(0)
		require.NoError(t, err)

		v, err := s.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)
	})

	runDB("get register", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		count := uint64(0)

		storageGet := func(id flow.RegisterID, height uint64) (flow.RegisterValue, error) {
			count++
			return value, nil
		}

		s := NewStorageSnapshot(storageGet, 0)

		v, err := s.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value, v)

		v, err = s.GetValue(owner, key)
		require.NoError(t, err)
		require.Equal(t, value, v)

		// value should be cached
		require.Equal(t, uint64(1), count)
	})

	runDB("get register - cache nil", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		count := uint64(0)

		storageGet := func(id flow.RegisterID, height uint64) (flow.RegisterValue, error) {
			count++
			return nil, nil
		}

		s := NewStorageSnapshot(storageGet, 0)

		v, err := s.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)

		v, err = s.GetValue(owner, key)
		require.NoError(t, err)
		require.Empty(t, v)

		// value should be cached
		require.Equal(t, uint64(1), count)
	})
}
