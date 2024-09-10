package pebble

import (
	"fmt"
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

		// registers is map[height][owner][key] = value
		registers := map[uint64]map[uint64]map[uint64]uint64{
			10: {
				100: {
					500: 1000,
					502: 2000,
					504: 3000,
				},
				101: {
					500: 3003,
					502: 2005,
					506: 4002,
				},
				102: {
					500: 4004,
				},
			},
			11: {
				100: {
					500: 1004,
				},
				102: {
					505: 4003,
				},
			},
			12: {
				104: {
					500: 1000,
				},
			},
			13: {
				100: {
					500: 1006,
				},
			},
			15: {
				102: {
					505: 4004,
				},
			},
		}

		for height, a := range registers {
			for owner, b := range a {
				for key, value := range b {
					owner := uint64Bytes(owner)
					key := uint64Bytes(key)
					val := uint64Bytes(value)

					reg := NewRegister(db, height)
					err := reg.SetValue(owner, key, val)
					require.NoError(t, err)
				}
			}
		}

		// expected is same map as registers but with heights after they were set
		// build up expected map which is used to verify results
		// it populates all the heights even if no value was set at that height,
		// it just uses last set value for register, this way we can make sure the
		// heights are correctly loaded.
		expected := make(map[uint64]map[uint64]map[uint64]uint64)
		for i := 5; i < 20; i++ {
			h := uint64(i)
			if i == 5 {
				expected[h] = make(map[uint64]map[uint64]uint64)
			} else { // copy previous values over to next height
				expected[h] = expected[h-1]
			}
			// update any new data
			for owner, a := range registers[h] {
				expected[h][owner] = make(map[uint64]uint64)
				for key, val := range a {
					expected[h][owner][key] = val
				}
			}
		}

		// make sure registers are correct even at the heights they weren't changed
		for height, a := range expected {
			reg := NewRegister(db, height)

			for owner, b := range a {
				for key, val := range b {
					o := uint64Bytes(owner)
					k := uint64Bytes(key)
					v := uint64Bytes(val)

					retVal, err := reg.GetValue(o, k)
					require.NoError(t, err)
					require.Equal(t, v, retVal, fmt.Sprintf("height: %d, owner: %d, key: %d, value: %d", height, owner, key, val))
				}
			}
		}
	})

}
