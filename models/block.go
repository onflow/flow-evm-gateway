package models

import (
	"fmt"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
)

type blockEventPayload struct {
	Height            uint64           `cadence:"height"`
	Hash              string           `cadence:"hash"`
	TotalSupply       cadence.Int      `cadence:"totalSupply"`
	ParentBlockHash   string           `cadence:"parentHash"`
	ReceiptRoot       string           `cadence:"receiptRoot"`
	TransactionHashes []cadence.String `cadence:"transactionHashes"`
}

// decodeBlock takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func decodeBlock(event cadence.Event) (*types.Block, error) {
	var b blockEventPayload
	err := cadence.DecodeFields(event, &b)
	if err != nil {
		return nil, fmt.Errorf("failed to cadence decode block: %w", err)
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
