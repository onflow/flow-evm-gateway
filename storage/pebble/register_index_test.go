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

		r := NewRegisters(db, ownerAddress)

		v, err := r.Get(flow.RegisterID{Owner: owner, Key: key}, 0)
		require.NoError(t, err)
		require.Empty(t, v)
	})

	runDB("get register - owner2", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisters(db, ownerAddress)

		_, err := r.Get(flow.RegisterID{Owner: owner2, Key: key}, 0)
		require.Error(t, err)
	})

	runDB("store registers", t, func(t *testing.T, db *Storage) {
		t.Parallel()

		r := NewRegisters(db, ownerAddress)

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

		r := NewRegisters(db, ownerAddress)

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
