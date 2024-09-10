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
func EqualReceipts(gethReceipt *gethTypes.Receipt, receipt *Receipt) (bool, []error) {
	errs := make([]error, 0)

	// fail if any receipt or both are nil
	if gethReceipt == nil || receipt == nil {
		errs = append(errs, fmt.Errorf("one or both receipts are nil"))
		return false, errs
	}
	// compare logs
	if len(gethReceipt.Logs) != len(receipt.Logs) {
		errs = append(errs, fmt.Errorf("log length mismatch: geth logs length %d, receipt logs length %d", len(gethReceipt.Logs), len(receipt.Logs)))
		return false, errs
	}

	// compare each log entry
	for i, l := range gethReceipt.Logs {
		rl := receipt.Logs[i]
		if rl.BlockNumber != l.BlockNumber {
			errs = append(errs, fmt.Errorf("log block number mismatch at index %d: %d != %d", i, rl.BlockNumber, l.BlockNumber))
		}
		if rl.Removed != l.Removed {
			errs = append(errs, fmt.Errorf("log removed status mismatch at index %d: %v != %v", i, rl.Removed, l.Removed))
		}
		if rl.TxHash.Cmp(l.TxHash) != 0 {
			errs = append(errs, fmt.Errorf("log TxHash mismatch at index %d", i))
		}
		if rl.Address.Cmp(l.Address) != 0 {
			errs = append(errs, fmt.Errorf("log address mismatch at index %d", i))
		}
		if !bytes.Equal(rl.Data, l.Data) {
			errs = append(errs, fmt.Errorf("log data mismatch at index %d", i))
		}
		if rl.TxIndex != l.TxIndex {
			errs = append(errs, fmt.Errorf("log transaction index mismatch at index %d: %d != %d", i, rl.TxIndex, l.TxIndex))
		}
		// compare all topics
		for j, t := range rl.Topics {
			if t.Cmp(l.Topics[j]) != 0 {
				errs = append(errs, fmt.Errorf("log topic mismatch at index %d, topic %d", i, j))
			}
		}
	}

	// compare all receipt data
	if gethReceipt.TxHash.Cmp(receipt.TxHash) != 0 {
		errs = append(errs, fmt.Errorf("receipt TxHash mismatch"))
	}
	if gethReceipt.GasUsed != receipt.GasUsed {
		errs = append(errs, fmt.Errorf("receipt GasUsed mismatch: %d != %d", gethReceipt.GasUsed, receipt.GasUsed))
	}
	if gethReceipt.CumulativeGasUsed != receipt.CumulativeGasUsed {
		errs = append(errs, fmt.Errorf("receipt CumulativeGasUsed mismatch: %d != %d", gethReceipt.CumulativeGasUsed, receipt.CumulativeGasUsed))
	}
	if gethReceipt.Type != 0 && gethReceipt.Type != receipt.Type { // only compare if not direct call
		errs = append(errs, fmt.Errorf("receipt Type mismatch: %d != %d", gethReceipt.Type, receipt.Type))
	}
	if gethReceipt.ContractAddress.Cmp(receipt.ContractAddress) != 0 {
		errs = append(errs, fmt.Errorf("receipt ContractAddress mismatch"))
	}
	if gethReceipt.Status != receipt.Status {
		errs = append(errs, fmt.Errorf("receipt Status mismatch: %d != %d", gethReceipt.Status, receipt.Status))
	}
	if !bytes.Equal(gethReceipt.Bloom.Bytes(), receipt.Bloom.Bytes()) {
		errs = append(errs, fmt.Errorf("receipt Bloom mismatch"))
	}

	return len(errs) == 0, errs
}

type BloomsHeight struct {
	Blooms []*gethTypes.Bloom
	Height uint64
}
