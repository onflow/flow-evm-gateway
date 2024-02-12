package pebble

import (
	"encoding/binary"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

const (
	// block codes
	codeHeightToBlock = byte(1)
	codeIDToBlock     = byte(2)

	// transaction codes
	codeIDToTx     = byte(10)
	codeHeightToTx = byte(11)

	// receipt codes
	codeTxIDToReceipt   = byte(20)
	codeHeightToReceipt = byte(21)
	codeHeightToBloom   = byte(22)

	// special codes
	codeLatestHeight = byte(100)
	codeFirstHeight  = byte(101)
)

func makePrefix(code byte, key any) []byte {
	prefix := make([]byte, 1)
	prefix[0] = code

	switch i := key.(type) {
	case uint64:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, i)
		return append(prefix, b...)
	case common.Hash:
		return append(prefix, i.Bytes()...)
	case *big.Int:
		return append(prefix, i.Bytes()...)
	default:
		panic(fmt.Sprintf("unsupported key type to convert (%T)", key))
	}
}
