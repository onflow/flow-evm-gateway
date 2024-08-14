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
	SafeBlockNumber      = big.NewInt(-4)
	FinalizedBlockNumber = big.NewInt(-3)
	LatestBlockNumber    = big.NewInt(-2)
	PendingBlockNumber   = big.NewInt(-1)
	EarliestBlockNumber  = big.NewInt(0)
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
		// todo temp remove this after previewnet is deprecated
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

// todo temp remove after previewnet is deprecated
// this will return an existing precalculated hash if it exsists
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
		},
	}, nil
}

// todo past decoding of blocks, moved from flow-go, remove after previewnet is deprecated

type blockV0 struct {
	ParentBlockHash gethCommon.Hash
	Height          uint64
	UUIDIndex       uint64
	TotalSupply     uint64
	StateRoot       gethCommon.Hash
	ReceiptRoot     gethCommon.Hash
}

// adds TransactionHashes

type blockV1 struct {
	ParentBlockHash   gethCommon.Hash
	Height            uint64
	UUIDIndex         uint64
	TotalSupply       uint64
	StateRoot         gethCommon.Hash
	ReceiptRoot       gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

// removes UUIDIndex

type blockV2 struct {
	ParentBlockHash   gethCommon.Hash
	Height            uint64
	TotalSupply       uint64
	StateRoot         gethCommon.Hash
	ReceiptRoot       gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

// removes state root

type blockV3 struct {
	ParentBlockHash   gethCommon.Hash
	Height            uint64
	TotalSupply       uint64
	ReceiptRoot       gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

// change total supply type

type blockV4 struct {
	ParentBlockHash   gethCommon.Hash
	Height            uint64
	TotalSupply       *big.Int
	ReceiptRoot       gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

// adds timestamp

type blockV5 struct {
	ParentBlockHash   gethCommon.Hash
	Height            uint64
	Timestamp         uint64
	TotalSupply       *big.Int
	ReceiptRoot       gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

// adds total gas used

type blockV6 struct {
	ParentBlockHash   gethCommon.Hash
	Height            uint64
	Timestamp         uint64
	TotalSupply       *big.Int
	ReceiptRoot       gethCommon.Hash
	TransactionHashes []gethCommon.Hash
	TotalGasUsed      uint64
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
				ParentBlockHash: b0.ParentBlockHash,
				Height:          b0.Height,
				ReceiptRoot:     b0.ReceiptRoot,
				TotalSupply:     big.NewInt(int64(b0.TotalSupply)),
			},
		}
	}

	b1 := &blockV1{}
	if err := rlp.DecodeBytes(encoded, b1); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash: b1.ParentBlockHash,
				Height:          b1.Height,
				TotalSupply:     big.NewInt(int64(b1.TotalSupply)),
				ReceiptRoot:     b1.ReceiptRoot,
			},
			TransactionHashes: b1.TransactionHashes,
		}
	}

	b2 := &blockV2{}
	if err := rlp.DecodeBytes(encoded, b2); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash: b2.ParentBlockHash,
				Height:          b2.Height,
				TotalSupply:     big.NewInt(int64(b2.TotalSupply)),
				ReceiptRoot:     b2.ReceiptRoot,
			},
			TransactionHashes: b2.TransactionHashes,
		}
	}

	b3 := &blockV3{}
	if err := rlp.DecodeBytes(encoded, b3); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash: b3.ParentBlockHash,
				Height:          b3.Height,
				TotalSupply:     big.NewInt(int64(b3.TotalSupply)),
				ReceiptRoot:     b3.ReceiptRoot,
			},
			TransactionHashes: b3.TransactionHashes,
		}
	}

	b4 := &blockV4{}
	if err := rlp.DecodeBytes(encoded, b4); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash: b4.ParentBlockHash,
				Height:          b4.Height,
				TotalSupply:     b4.TotalSupply,
				ReceiptRoot:     b4.ReceiptRoot,
			},
			TransactionHashes: b4.TransactionHashes,
		}
	}

	b5 := &blockV5{}
	if err := rlp.DecodeBytes(encoded, b5); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash: b5.ParentBlockHash,
				Height:          b5.Height,
				Timestamp:       b5.Timestamp,
				TotalSupply:     b5.TotalSupply,
				ReceiptRoot:     b5.ReceiptRoot,
			},
			TransactionHashes: b5.TransactionHashes,
		}
	}

	b6 := &blockV6{}
	if err := rlp.DecodeBytes(encoded, b6); err == nil {
		return &Block{
			Block: &types.Block{
				ParentBlockHash: b6.ParentBlockHash,
				Height:          b6.Height,
				Timestamp:       b6.Timestamp,
				TotalSupply:     b6.TotalSupply,
				ReceiptRoot:     b6.ReceiptRoot,
				TotalGasUsed:    b6.TotalGasUsed,
			},
			TransactionHashes: b6.TransactionHashes,
		}
	}

	return nil
}
