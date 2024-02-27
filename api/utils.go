package api

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
)

var byteArrayType = cadence.NewVariableSizedArrayType(cadence.UInt8Type{})

var evmAddressType = cadence.NewConstantSizedArrayType(
	common.AddressLength,
	cadence.UInt8Type{},
)

func GethTxFromBytes(input hexutil.Bytes) (*types.Transaction, error) {
	gethTx := &types.Transaction{}
	err := gethTx.DecodeRLP(
		rlp.NewStream(
			bytes.NewReader(input),
			uint64(len(input)),
		),
	)

	return gethTx, err
}

func CadenceByteArrayFromBytes(input []byte) cadence.Array {
	values := make([]cadence.Value, 0)
	for _, element := range input {
		values = append(values, cadence.UInt8(element))
	}

	return cadence.NewArray(values).WithType(byteArrayType)
}

func CadenceEVMAddressFromBytes(input []byte) cadence.Array {
	values := make([]cadence.Value, 0)
	for _, element := range input {
		values = append(values, cadence.UInt8(element))
	}

	return cadence.NewArray(values).WithType(evmAddressType)
}
