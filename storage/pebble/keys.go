package pebble

import "encoding/binary"

const (
	// block keys
	blockHeightKey   = byte(1)
	blockIDHeightKey = byte(2)

	// transaction keys
	txIDKey = byte(10)

	// receipt keys
	receiptTxIDKey   = byte(20)
	receiptHeightKey = byte(21)
	bloomHeightKey   = byte(22)

	// special keys
	latestHeightKey = byte(100)
	firstHeightKey  = byte(101)
)

func makePrefix(code byte, key ...[]byte) []byte {
	prefix := make([]byte, 1)
	prefix[0] = code

	// allow for special keys
	if key == nil {
		return prefix
	}
	if len(key) != 1 {
		panic("unsupported key length")
	}

	return append(prefix, key[0]...)
}

func uint64Bytes(height uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, height)
	return b
}
