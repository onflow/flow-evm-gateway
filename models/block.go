package models

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/cadence"
	cdcCommon "github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var blockExecutedType = (types.EVMLocation{}).TypeID(nil, string(types.EventTypeBlockExecuted))

type blockEventPayload struct {
	Height            uint64           `cadence:"height"`
	Hash              string           `cadence:"hash"`
	TotalSupply       cadence.Int      `cadence:"totalSupply"`
	ParentBlockHash   string           `cadence:"parentHash"`
	ReceiptRoot       string           `cadence:"receiptRoot"`
	TransactionHashes []cadence.String `cadence:"transactionHashes"`
}

// DecodeBlock takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func DecodeBlock(event cadence.Event) (*types.Block, error) {
	if cdcCommon.TypeID(event.EventType.ID()) != blockExecutedType {
		return nil, fmt.Errorf(
			"invalid event type for decoding into block executed event, received %s expected %s",
			event.Type().ID(),
			types.EventTypeBlockExecuted,
		)
	}

	b := blockEventPayload{}
	err := cadence.DecodeFields(event, &b)
	if err != nil {
		return nil, err
	}

	hashes := make([]common.Hash, len(b.TransactionHashes))
	for i, h := range b.TransactionHashes {
		hashes[i] = common.HexToHash(h.ToGoValue().(string))
	}

	return &types.Block{
		ParentBlockHash:   common.HexToHash(b.ParentBlockHash),
		Height:            b.Height,
		TotalSupply:       b.TotalSupply.Value,
		ReceiptRoot:       common.HexToHash(b.ReceiptRoot),
		TransactionHashes: hashes,
	}, nil
}
