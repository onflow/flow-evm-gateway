package models

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

// StorageReceipt is a receipt representation for storage.
//
// This struct copies the geth.Receipt type found here: https://github.com/ethereum/go-ethereum/blob/9bbb9df18549d6f81c3d1f4fc6c65f71bc92490d/core/types/receipt.go#L52
// the reason is if we use geth.Receipt some values will be skipped when RLP encoding which is because
// geth node has the data locally, but we don't in evm gateway, so we can not reproduce those values
// and we need to store them
type StorageReceipt struct {
	Type              uint8
	PostState         []byte
	Status            uint64
	CumulativeGasUsed uint64
	// todo we could skip bloom to optimize storage and dynamically recalculate it
	Bloom             gethTypes.Bloom
	Logs              []*gethTypes.Log
	TxHash            common.Hash
	ContractAddress   common.Address
	GasUsed           uint64
	EffectiveGasPrice *big.Int
	BlobGasUsed       uint64
	BlobGasPrice      *big.Int
	BlockHash         common.Hash
	BlockNumber       *big.Int
	TransactionIndex  uint
}
