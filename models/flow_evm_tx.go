package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type FlowEVMTxData interface {
	Hash() (common.Hash, error)
	RawSignatureValues() (v *big.Int, r *big.Int, s *big.Int)
	From() (common.Address, error)
	To() *common.Address
	Data() []byte
	Nonce() uint64
	Value() *big.Int
	Type() uint8
	Gas() uint64
	GasPrice() *big.Int
	BlobGas() uint64
	Size() uint64
	MarshalBinary() ([]byte, error)
}
