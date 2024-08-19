package models

import (
	"math/big"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	gethTypes "github.com/onflow/go-ethereum/core/types"
)

// StorageReceipt is a receipt representation for storage.
//
// This struct copies the geth.Receipt type found here: https://github.com/ethereum/go-ethereum/blob/9bbb9df18549d6f81c3d1f4fc6c65f71bc92490d/core/types/receipt.go#L52
// the reason is if we use geth.Receipt some values will be skipped when RLP encoding which is because
// geth node has the data locally, but we don't in evm gateway, so we can not reproduce those values
// and we need to store them
type StorageReceipt struct {
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

func (sr *StorageReceipt) ToGethReceipt() *gethTypes.Receipt {
	return &gethTypes.Receipt{
		Type:              sr.Type,
		PostState:         sr.PostState,
		Status:            sr.Status,
		CumulativeGasUsed: sr.CumulativeGasUsed,
		Bloom:             sr.Bloom,
		Logs:              sr.Logs,
		TxHash:            sr.TxHash,
		ContractAddress:   sr.ContractAddress,
		GasUsed:           sr.GasUsed,
		EffectiveGasPrice: sr.EffectiveGasPrice,
		BlobGasUsed:       sr.BlobGasUsed,
		BlobGasPrice:      sr.BlobGasPrice,
		BlockHash:         sr.BlockHash,
		BlockNumber:       sr.BlockNumber,
		TransactionIndex:  sr.TransactionIndex,
	}
}

func NewStorageReceipt(
	receipt *gethTypes.Receipt,
	revertReason []byte,
	precompiledCalls []byte,
) *StorageReceipt {
	return &StorageReceipt{
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

// MarshalReceipt takes a receipt and its associated transaction,
// and marshals the receipt to the proper structure needed by
// eth_getTransactionReceipt.
func MarshalReceipt(
	receipt *StorageReceipt,
	tx Transaction,
) (map[string]interface{}, error) {
	from, err := tx.From()
	if err != nil {
		return map[string]interface{}{}, err
	}

	txHash := tx.Hash()

	fields := map[string]interface{}{
		"blockHash":         receipt.BlockHash,
		"blockNumber":       hexutil.Uint64(receipt.BlockNumber.Uint64()),
		"transactionHash":   txHash,
		"transactionIndex":  hexutil.Uint64(receipt.TransactionIndex),
		"from":              from.Hex(),
		"to":                nil,
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"contractAddress":   nil,
		"logs":              receipt.Logs,
		"logsBloom":         receipt.Bloom,
		"type":              hexutil.Uint(tx.Type()),
		"effectiveGasPrice": (*hexutil.Big)(receipt.EffectiveGasPrice),
	}

	if tx.To() != nil {
		fields["to"] = tx.To().Hex()
	}

	fields["status"] = hexutil.Uint(receipt.Status)

	if receipt.Logs == nil {
		fields["logs"] = []*gethTypes.Log{}
	}

	if tx.Type() == gethTypes.BlobTxType {
		fields["blobGasUsed"] = hexutil.Uint64(receipt.BlobGasUsed)
		fields["blobGasPrice"] = (*hexutil.Big)(receipt.BlobGasPrice)
	}

	// If the ContractAddress is 20 0x0 bytes, assume it is not a contract creation
	if receipt.ContractAddress != (common.Address{}) {
		fields["contractAddress"] = receipt.ContractAddress.Hex()
	}

	if len(receipt.RevertReason) > 0 {
		fields["revertReason"] = hexutil.Bytes(receipt.RevertReason)
	}

	return fields, nil
}

type BloomsHeight struct {
	Blooms []*gethTypes.Bloom
	Height uint64
}
