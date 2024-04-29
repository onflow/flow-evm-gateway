package models

import (
	"fmt"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
)

// decodeBlock takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func decodeBlock(event cadence.Event) (*types.Block, error) {
	payload, err := types.DecodeBlockEventPayload(event)
	if err != nil {
		return nil, fmt.Errorf("failed to cadence decode block: %w", err)
	}

	hashes := make([]common.Hash, len(payload.TransactionHashes))
	for i, h := range payload.TransactionHashes {
		hashes[i] = common.HexToHash(h.ToGoValue().(string))
	}

	return &types.Block{
		ParentBlockHash:   common.HexToHash(payload.ParentBlockHash),
		Height:            payload.Height,
		Timestamp:         payload.Timestamp,
		TotalSupply:       payload.TotalSupply.Value,
		ReceiptRoot:       common.HexToHash(payload.ReceiptRoot),
		TransactionHashes: hashes,
		TotalGasUsed:      payload.TotalGasUsed,
	}, nil
}
