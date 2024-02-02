package models

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
	cdcCommon "github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go/fvm/evm/types"
	"math/big"
)

var txExecutedType = (types.EVMLocation{}).TypeID(nil, string(types.EventTypeTransactionExecuted))

type txEventPayload struct {
	BlockHeight             uint64 `cadence:"blockHeight"`
	TransactionHash         string `cadence:"transactionHash"`
	Transaction             string `cadence:"transaction"`
	TransactionType         int    `cadence:"transactionType"`
	Failed                  bool   `cadence:"failed"`
	GasConsumed             uint64 `cadence:"gasConsumed"`
	DeployedContractAddress string `cadence:"deployedContractAddress"`
	ReturnedValue           string `cadence:"returnedValue"`
	Logs                    string `cadence:"logs"`
}

// DecodeReceipt takes a cadence event for transaction executed and decodes it into the receipt.
func DecodeReceipt(event cadence.Event) (*gethTypes.Receipt, error) {
	if cdcCommon.TypeID(event.EventType.ID()) != txExecutedType {
		return nil, fmt.Errorf(
			"invalid event type for decoding into receipt, received %s expected %s",
			event.Type().ID(),
			types.EventTypeTransactionExecuted,
		)
	}

	var tx txEventPayload
	err := cadence.DecodeFields(event, &tx)
	if err != nil {
		return nil, err
	}

	encLogs, err := hex.DecodeString(tx.Logs)
	if err != nil {
		return nil, err
	}

	var logs []*gethTypes.Log
	err = rlp.Decode(bytes.NewReader(encLogs), &logs)
	if err != nil {
		return nil, err
	}

	receipt := &gethTypes.Receipt{
		Type:              0,              // todo check
		Status:            0,              // todo check
		CumulativeGasUsed: tx.GasConsumed, // todo check
		Logs:              logs,
		TxHash:            common.HexToHash(tx.TransactionHash),
		ContractAddress:   common.HexToAddress(tx.DeployedContractAddress),
		GasUsed:           tx.GasConsumed,
		EffectiveGasPrice: nil,           // todo check
		BlobGasUsed:       0,             // todo check
		BlobGasPrice:      nil,           // todo check
		BlockHash:         common.Hash{}, // todo check
		BlockNumber:       big.NewInt(int64(tx.BlockHeight)),
		TransactionIndex:  0, // todo check
	}

	receipt.Bloom = gethTypes.CreateBloom([]*gethTypes.Receipt{receipt})

	return receipt, nil
}

// DecodeTransaction takes a cadence event for transaction executed and decodes it into the transaction.
func DecodeTransaction(event cadence.Event) (*gethTypes.Transaction, error) {
	if cdcCommon.TypeID(event.EventType.ID()) != txExecutedType {
		return nil, fmt.Errorf(
			"invalid event type for decoding into receipt, received %s expected %s",
			event.Type().ID(),
			types.EventTypeTransactionExecuted,
		)
	}

	var t txEventPayload
	err := cadence.DecodeFields(event, &t)
	if err != nil {
		return nil, err
	}

	encTx, err := hex.DecodeString(t.Transaction)
	if err != nil {
		return nil, err
	}

	tx := gethTypes.Transaction{}
	err = tx.DecodeRLP(rlp.NewStream(bytes.NewReader(encTx), uint64(len(encTx))))
	if err != nil {
		return nil, err
	}

	return &tx, nil
}
