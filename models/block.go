package models

import (
	"fmt"
	"math/big"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/rlp"
)

var (
	LatestBlockNumber   = big.NewInt(-2)
	EarliestBlockNumber = big.NewInt(0)
)

func GenesisBlock(chainID flow.ChainID) *Block {
	return &Block{
		Block:             types.GenesisBlock(chainID),
		TransactionHashes: []gethCommon.Hash{},
	}
}

func NewBlockFromBytes(data []byte) (*Block, error) {
	var b *Block
	err := rlp.DecodeBytes(data, &b)
	if err != nil {
		return nil, err
	}

	return b, nil
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
			PrevRandao:          payload.PrevRandao,
		},
	}, nil
}
