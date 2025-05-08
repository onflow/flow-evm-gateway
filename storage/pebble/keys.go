package pebble

import (
	"encoding/binary"

	"github.com/cockroachdb/pebble"
)

const (
	// block keys
	blockHeightKey              = byte(1)
	blockIDToHeightKey          = byte(2)
	evmHeightToCadenceHeightKey = byte(3)
	evmHeightToCadenceIDKey     = byte(4)

	// transaction keys
	txIDKey = byte(10)

	// receipt keys
	receiptTxIDToHeightKey = byte(20)
	receiptHeightKey       = byte(21)
	bloomHeightKey         = byte(22)

	// traces keys
	traceTxIDKey = byte(40)

	// registers
	registerKeyMarker = byte(50)

	// special keys
	latestEVMHeightKey     = byte(100)
	latestCadenceHeightKey = byte(102)

	eventsHashKey         = byte(150)
	sealedEventsHeightKey = byte(151)
)

// makePrefix makes a key used internally to store the values
func makePrefix(code byte, key ...[]byte) []byte {
	prefix := make([]byte, 1)
	prefix[0] = code

	// allow for special keys
	if len(key) == 0 {
		return prefix
	}
	if len(key) != 1 {
		panic("unsupported key length")
	}

	return append(prefix, key[0]...)
}

// stripPrefix from the key and return only the key value
func stripPrefix(key []byte) []byte {
	return key[1:]
}

// uint64Bytes converts the uint64 value to bytes
func uint64Bytes(height uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, height)
	return b
}

func NewMVCCComparer() *pebble.Comparer {
	comparer := *pebble.DefaultComparer
	comparer.Split = func(a []byte) int {
		if len(a) == 0 {
			// edge case. Not sure if this is possible, but just in case
			return 0
		}
		if a[0] == registerKeyMarker {
			// special case for registers
			return len(a) - 8
		}
		// default comparer
		return len(a)
	}
	comparer.Name = "flow.MVCCComparer"

	return &comparer
}
