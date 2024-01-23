package storage_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

type DirectCall struct {
	Type     byte
	SubType  byte
	From     common.Address
	To       common.Address
	Data     []byte
	Value    *big.Int
	GasLimit uint64
}

// Encode encodes the direct call it also adds the type
// as the very first byte, similar to how evm encodes types.
func (dc *DirectCall) Encode() ([]byte, error) {
	encoded, err := rlp.EncodeToBytes(dc)
	return append([]byte{dc.Type}, encoded...), err
}

// Hash computes the hash of a direct call
func (dc *DirectCall) Hash() (common.Hash, error) {
	encoded, err := dc.Encode()
	return crypto.Keccak256Hash(encoded), err
}

func TestRLPDecode(t *testing.T) {
	data := "f85af85894522b3294e6d06aa25ad0f1b8891242e335d3b459e1a024abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503daa0000000000000000000000000000000000000000000000000000000000000002a"
	bt, _ := hex.DecodeString(data)
	log := []*types.Log{}
	err := rlp.Decode(bytes.NewReader(bt), &log)
	if err != nil {
		panic(err)
	}
	fmt.Println("LOG: ", log[0])
	assert.Equal(
		t,
		"0x0000000000000000000000000000000000000000",
		common.Address{}.Hex(),
	)
}

func TestDeployDirectCall(t *testing.T) {
	hexData := "6060604052341561000f57600080fd5b60eb8061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063c6888fa1146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60007f24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da600783026040518082815260200191505060405180910390a16007820290509190505600a165627a7a7230582040383f19d9f65246752244189b02f56e8d0980ed44e7a56c0b200458caad20bb0029"
	data, err := hex.DecodeString(hexData)
	if err != nil {
		panic(err)
	}
	dc := DirectCall{
		Type:     byte(255),
		SubType:  byte(4),
		From:     common.HexToAddress("0x0000000000000000000000000000000000000001"),
		To:       common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Data:     data,
		Value:    big.NewInt(0),
		GasLimit: 1200000,
	}

	encoded, err := dc.Encode()
	if err != nil {
		panic(err)
	}

	expected := "fff9013d81ff04940000000000000000000000000000000000000001940000000000000000000000000000000000000000b901086060604052341561000f57600080fd5b60eb8061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063c6888fa1146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60007f24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da600783026040518082815260200191505060405180910390a16007820290509190505600a165627a7a7230582040383f19d9f65246752244189b02f56e8d0980ed44e7a56c0b200458caad20bb00298083124f80"
	actual := hex.EncodeToString(encoded)
	assert.Equal(t, expected, actual)

	expectedHash := "0x7ba223b9b60d29c33aeeb35110a0302bbe839c29845f21c616dab8b662f44dc2"
	actualHash, err := dc.Hash()
	if err != nil {
		panic(err)
	}
	assert.Equal(t, expectedHash, actualHash.Hex())

	//assert.Fail(t, "for some reason")
}
