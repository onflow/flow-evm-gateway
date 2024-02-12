package pebble

import (
	"encoding/binary"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

const (
	// block keys
	blockHeightKey = byte(1)
	blockIDKey     = byte(2)

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

func makePrefix(code byte, key ...any) []byte {
	prefix := make([]byte, 1)
	prefix[0] = code

	// allow for special keys
	if key == nil {
		return prefix
	}
	if len(key) != 1 {
		panic("unsupported key length")
	}

	switch i := key[0].(type) {
	case uint64:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, i)
		return append(prefix, b...)
	case common.Hash:
		return append(prefix, i.Bytes()...)
	case *big.Int:
		return append(prefix, i.Bytes()...)
	default:
		panic(fmt.Sprintf("unsupported key type to convert (%T)", key[0]))
	}
}
