package models

import (
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/rlp"
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

func ReceiptsFromBytes(data []byte) ([]*Receipt, error) {
	var receipts []*Receipt
	if err := rlp.DecodeBytes(data, &receipts); err != nil {
		receipts, err = legacyReceiptFromBytes(data)
		if err == nil {
			return receipts, nil
		}

		return nil, fmt.Errorf("failed to RLP-decode block receipts [%x] %w", data, err)
	}
	return receipts, nil
}

type BloomsHeight struct {
	Blooms []*gethTypes.Bloom
	Height uint64
}

// decode legacy receipts, todo can be remove after re-indexed on testnet
func legacyReceiptFromBytes(data []byte) ([]*Receipt, error) {
	var receiptsV0 []*receiptV0
	if err := rlp.DecodeBytes(data, &receiptsV0); err != nil {
		return nil, err
	}

	receipts := make([]*Receipt, len(receiptsV0))
	for i, r := range receiptsV0 {
		receipts[i] = &Receipt{
			Type:              r.Type,
			Status:            r.Status,
			CumulativeGasUsed: r.CumulativeGasUsed,
			Bloom:             r.Bloom,
			Logs:              r.Logs,
			TxHash:            r.TxHash,
			ContractAddress:   r.ContractAddress,
			GasUsed:           r.GasUsed,
			EffectiveGasPrice: r.EffectiveGasPrice,
			BlockHash:         r.BlockHash,
			BlockNumber:       r.BlockNumber,
			TransactionIndex:  r.TransactionIndex,
			RevertReason:      r.RevertReason,
		}
	}
	return receipts, nil
}

type receiptV0 struct {
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
}
