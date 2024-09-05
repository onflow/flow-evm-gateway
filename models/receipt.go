package models

import (
	"bytes"
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
		return nil, fmt.Errorf("failed to RLP-decode block receipts [%x] %w", data, err)
	}
	return receipts, nil
}

// EqualReceipts takes a geth Receipt type and EVM GW receipt and compares all the applicable values.
func EqualReceipts(gethReceipt *gethTypes.Receipt, receipt *Receipt) bool {
	// fail if any receipt or both are nil
	if gethReceipt == nil || receipt == nil {
		return false
	}
	// compare logs
	if len(gethReceipt.Logs) != len(receipt.Logs) {
		return false
	}

	// todo block data might not be present, investigate
	for i, l := range gethReceipt.Logs {
		rl := receipt.Logs[i]
		if rl.BlockNumber != l.BlockNumber ||
			rl.Removed != l.Removed ||
			rl.TxHash.Cmp(l.TxHash) != 0 ||
			rl.Address.Cmp(l.Address) != 0 ||
			rl.BlockHash.Cmp(l.BlockHash) != 0 ||
			rl.Index != l.Index ||
			bytes.Equal(rl.Data, l.Data) == false ||
			rl.TxIndex != l.TxIndex {
			return false
		}
		// compare all topics
		for j, t := range rl.Topics {
			if t.Cmp(l.Topics[j]) != 0 {
				return false
			}
		}
	}

	// compare all receipt data
	return gethReceipt.TxHash.Cmp(receipt.TxHash) == 0 &&
		gethReceipt.GasUsed == receipt.GasUsed &&
		gethReceipt.CumulativeGasUsed == receipt.CumulativeGasUsed &&
		gethReceipt.Type == receipt.Type &&
		gethReceipt.ContractAddress.Cmp(receipt.ContractAddress) == 0 &&
		gethReceipt.Status == receipt.Status &&
		bytes.Equal(gethReceipt.Bloom.Bytes(), receipt.Bloom.Bytes())
	// todo there are other fields, should we compare, do we have block number?
}

type BloomsHeight struct {
	Blooms []*gethTypes.Bloom
	Height uint64
}
