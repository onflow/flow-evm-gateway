package models

import (
	"fmt"
	"math/big"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var (
	SafeBlockNumber      = big.NewInt(-4)
	FinalizedBlockNumber = big.NewInt(-3)
	LatestBlockNumber    = big.NewInt(-2)
	PendingBlockNumber   = big.NewInt(-1)
	EarliestBlockNumber  = big.NewInt(0)
)

// decodeBlock takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func decodeBlock(event cadence.Event) (*types.Block, error) {
	payload, err := types.DecodeBlockEventPayload(event)
	if err != nil {
		return nil, fmt.Errorf("failed to cadence decode block [%s]: %w", event.String(), err)
	}

	return &types.Block{
		ParentBlockHash:   payload.ParentBlockHash,
		Height:            payload.Height,
		Timestamp:         payload.Timestamp,
		TotalSupply:       payload.TotalSupply.Value,
		ReceiptRoot:       payload.ReceiptRoot,
		TransactionHashes: payload.TransactionHashes,
		TotalGasUsed:      payload.TotalGasUsed,
	}, nil
}
