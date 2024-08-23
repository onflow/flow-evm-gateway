package models

import (
	"fmt"
	"math/big"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethCrypto "github.com/onflow/go-ethereum/crypto"
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
		pastBlock := decodeBlockBreakingChanges(data)
		if pastBlock == nil {
			return nil, err
		}
		h, err := pastBlockHash(pastBlock)
		if err != nil {
			return nil, err
		}
		b = pastBlock
		b.hash = &h
	}

	return b, nil
}

type Block struct {
	*types.Block
	hash              *gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

func (b *Block) Hash() (gethCommon.Hash, error) {
	if b.hash != nil {
		return *b.hash, nil
	}
	return b.Block.Hash()
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

type blockV0 struct {
	ParentBlockHash     gethCommon.Hash
	Height              uint64
	Timestamp           uint64
	TotalSupply         *big.Int
	ReceiptRoot         gethCommon.Hash
	TransactionHashRoot gethCommon.Hash
	TotalGasUsed        uint64
}

// Adds PrevRandao field
type blockV1 struct {
	ParentBlockHash     gethCommon.Hash
	Height              uint64
	Timestamp           uint64
	TotalSupply         *big.Int
	ReceiptRoot         gethCommon.Hash
	TransactionHashRoot gethCommon.Hash
	TotalGasUsed        uint64
	PrevRandao          gethCommon.Hash
}

func pastBlockHash(b any) (gethCommon.Hash, error) {
	data, err := rlp.EncodeToBytes(b)
	return gethCrypto.Keccak256Hash(data), err
}

// decodeBlockBreakingChanges will try to decode the bytes into all
// previous versions of block type, if it succeeds it will return the
// migrated block, otherwise it will return nil.
func decodeBlockBreakingChanges(encoded []byte) *Block {
	b0 := &blockV0{}
	if err := rlp.DecodeBytes(encoded, b0); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash:     b0.ParentBlockHash,
				Height:              b0.Height,
				Timestamp:           b0.Timestamp,
				TotalSupply:         b0.TotalSupply,
				ReceiptRoot:         b0.ReceiptRoot,
				TransactionHashRoot: b0.TransactionHashRoot,
				TotalGasUsed:        b0.TotalGasUsed,
			},
		}
	}

	b1 := &blockV1{}
	if err := rlp.DecodeBytes(encoded, b1); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash:     b1.ParentBlockHash,
				Height:              b1.Height,
				Timestamp:           b1.Timestamp,
				TotalSupply:         b1.TotalSupply,
				ReceiptRoot:         b1.ReceiptRoot,
				TransactionHashRoot: b1.TransactionHashRoot,
				TotalGasUsed:        b1.TotalGasUsed,
				PrevRandao:          b1.PrevRandao,
			},
		}
	}

	return nil
}
