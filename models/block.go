package models

import (
	"fmt"
	"math/big"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/rlp"
)

var (
	SafeBlockNumber      = big.NewInt(-4)
	FinalizedBlockNumber = big.NewInt(-3)
	LatestBlockNumber    = big.NewInt(-2)
	PendingBlockNumber   = big.NewInt(-1)
	EarliestBlockNumber  = big.NewInt(0)
)

var GenesisBlock = &Block{Block: types.GenesisBlock}

func NewBlockFromBytes(data []byte) (*Block, error) {
	var b *Block
	return b, rlp.DecodeBytes(data, &b)
}

type Block struct {
	*types.Block
	TransactionHashes []gethCommon.Hash
}

func (b *Block) ToBytes() ([]byte, error) {
	return rlp.EncodeToBytes(b)
}

// decodeBlockEvent takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func decodeBlockEvent(event cadence.Event) (*Block, error) {
	payload, err := events.DecodeBlockEventPayload(event)
	if err != nil {
		return nil, fmt.Errorf("failed to cadence decode block [%s]: %w", event.String(), err)
	}

	return &Block{
		Block: &types.Block{
			ParentBlockHash:     payload.ParentBlockHash,
			Height:              payload.Height,
			Timestamp:           payload.Timestamp,
			TotalSupply:         payload.TotalSupply.Value,
			ReceiptRoot:         payload.ReceiptRoot,
			TransactionHashRoot: payload.TransactionHashRoot,
			TotalGasUsed:        payload.TotalGasUsed,
		},
	}, nil
}
