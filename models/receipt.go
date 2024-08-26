package models

import (
	"math/big"

	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
)

// Receipt struct copies the geth.Receipt type found here:
// https://github.com/ethereum/go-ethereum/blob/9bbb9df18549d6f81c3d1f4fc6c65f71bc92490d/core/types/receipt.go#L52
//
// the reason is if we use geth.Receipt some values will be skipped when RLP encoding which is because
// geth node has the data locally, but we don't in evm gateway, so we can not reproduce those values
// and we need to store them
type Receipt struct {
	Type              uint8            `json:"type,omitempty"`
	PostState         []byte           `json:"root"`
	Status            uint64           `json:"status"`
	CumulativeGasUsed uint64           `json:"cumulativeGasUsed"`
	Bloom             gethTypes.Bloom  `json:"logsBloom"`
	Logs              []*gethTypes.Log `json:"logs"`
	TxHash            common.Hash      `json:"transactionHash"`
	ContractAddress   common.Address   `json:"contractAddress"`
	GasUsed           uint64           `json:"gasUsed"`
	EffectiveGasPrice *big.Int         `json:"effectiveGasPrice"`
	BlobGasUsed       uint64           `json:"blobGasUsed,omitempty"`
	BlobGasPrice      *big.Int         `json:"blobGasPrice,omitempty"`
	BlockHash         common.Hash      `json:"blockHash,omitempty"`
	BlockNumber       *big.Int         `json:"blockNumber,omitempty"`
	TransactionIndex  uint             `json:"transactionIndex"`
	RevertReason      []byte           `json:"revertReason"`
	PrecompiledCalls  []byte
}

func NewReceipt(
	receipt *gethTypes.Receipt,
	revertReason []byte,
	precompiledCalls []byte,
) *Receipt {
	return &Receipt{
		Type:              receipt.Type,
		PostState:         receipt.PostState,
		Status:            receipt.Status,
		CumulativeGasUsed: receipt.CumulativeGasUsed,
		Bloom:             receipt.Bloom,
		Logs:              receipt.Logs,
		TxHash:            receipt.TxHash,
		ContractAddress:   receipt.ContractAddress,
		GasUsed:           receipt.GasUsed,
		EffectiveGasPrice: receipt.EffectiveGasPrice,
		BlobGasUsed:       receipt.BlobGasUsed,
		BlobGasPrice:      receipt.BlobGasPrice,
		BlockHash:         receipt.BlockHash,
		BlockNumber:       receipt.BlockNumber,
		TransactionIndex:  receipt.TransactionIndex,
		RevertReason:      revertReason,
		PrecompiledCalls:  precompiledCalls,
	}
}

func ReceiptsFromBytes(data []byte) {

}

type BloomsHeight struct {
	Blooms []*gethTypes.Bloom
	Height uint64
}
