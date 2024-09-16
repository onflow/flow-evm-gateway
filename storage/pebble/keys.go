package pebble

import "encoding/binary"

const (
	// block keys
	blockHeightKey              = byte(1)
	blockIDToHeightKey          = byte(2)
	evmHeightToCadenceHeightKey = byte(3)
	evmHeightToCadenceIDKey     = byte(4)
	evmHeightIndex              = byte(5)

	// transaction keys
	txIDKey = byte(10)

	// receipt keys
	receiptTxIDToHeightKey = byte(20)
	receiptHeightKey       = byte(21)
	bloomHeightKey         = byte(22)

	// account keys
	accountNonceKey   = byte(30)
	accountBalanceKey = byte(31)

	// traces keys
	traceTxIDKey = byte(40)

	// ledger value
	ledgerValue     = byte(50)
	ledgerSlabIndex = byte(51)

	// special keys
	latestEVMHeightKey     = byte(100)
	latestCadenceHeightKey = byte(102)
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
