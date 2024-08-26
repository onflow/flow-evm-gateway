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
		pastBlock, err := decodeBlockBreakingChanges(data)
		if err != nil {
			return nil, err
		}
		b = pastBlock
	}

	return b, nil
}

type Block struct {
	*types.Block
	hash              *gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

func (b *Block) ToBytes() ([]byte, error) {
	return rlp.EncodeToBytes(b)
}

func (b *Block) Hash() (gethCommon.Hash, error) {
	if b.hash != nil {
		return *b.hash, nil
	}
	return b.Block.Hash()
}

// decodeBlockEvent takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func decodeBlockEvent(event cadence.Event) (*Block, error) {
	payload, err := events.DecodeBlockEventPayload(event)
	if err != nil {
		if block, err := decodeLegacyBlockEvent(event); err == nil {
			return block, nil
		}

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

// todo remove this after updated in flow-go
type blockEventPayloadV0 struct {
	Height              uint64          `cadence:"height"`
	Hash                gethCommon.Hash `cadence:"hash"`
	Timestamp           uint64          `cadence:"timestamp"`
	TotalSupply         cadence.Int     `cadence:"totalSupply"`
	TotalGasUsed        uint64          `cadence:"totalGasUsed"`
	ParentBlockHash     gethCommon.Hash `cadence:"parentHash"`
	ReceiptRoot         gethCommon.Hash `cadence:"receiptRoot"`
	TransactionHashRoot gethCommon.Hash `cadence:"transactionHashRoot"`
}

// DecodeBlockEventPayload decodes Cadence event into block event payload.
func decodeLegacyBlockEvent(event cadence.Event) (*Block, error) {
	var block blockEventPayloadV0
	err := cadence.DecodeFields(event, &block)
	if err != nil {
		return nil, err
	}

	return &Block{
		Block: &types.Block{
			ParentBlockHash:     block.ParentBlockHash,
			Height:              block.Height,
			Timestamp:           block.Timestamp,
			TotalSupply:         block.TotalSupply.Value,
			ReceiptRoot:         block.ReceiptRoot,
			TransactionHashRoot: block.TransactionHashRoot,
			TotalGasUsed:        block.TotalGasUsed,
		},
	}, nil
}

// blockV0 is the block format, prior to adding the PrevRandao field.
type blockV0 struct {
	Block             *blockV0Fields
	TransactionHashes []gethCommon.Hash
}

// Hash returns the hash of the block, taking into account only
// the fields from the blockV0Fields type.
func (b *blockV0) Hash() (gethCommon.Hash, error) {
	data, err := b.Block.ToBytes()
	return gethCrypto.Keccak256Hash(data), err
}

// blockV0Fields needed for decoding & computing the hash of blocks
// prior to the addition of PrevRandao field.
type blockV0Fields struct {
	ParentBlockHash     gethCommon.Hash
	Height              uint64
	Timestamp           uint64
	TotalSupply         *big.Int
	ReceiptRoot         gethCommon.Hash
	TransactionHashRoot gethCommon.Hash
	TotalGasUsed        uint64
}

// ToBytes encodes the block fields into bytes.
func (b *blockV0Fields) ToBytes() ([]byte, error) {
	return rlp.EncodeToBytes(b)
}

// decodeBlockBreakingChanges will try to decode the bytes into all
// previous versions of block type, if it succeeds it will return the
// migrated block, otherwise it will return the decoding error.
func decodeBlockBreakingChanges(encoded []byte) (*Block, error) {
	b0 := &blockV0{}
	err := rlp.DecodeBytes(encoded, b0)
	if err != nil {
		return nil, err
	}

	blockHash, err := b0.Hash()
	if err != nil {
		return nil, err
	}

	return &Block{
		Block: &types.Block{
			ParentBlockHash:     b0.Block.ParentBlockHash,
			Height:              b0.Block.Height,
			Timestamp:           b0.Block.Timestamp,
			TotalSupply:         b0.Block.TotalSupply,
			ReceiptRoot:         b0.Block.ReceiptRoot,
			TransactionHashRoot: b0.Block.TransactionHashRoot,
			TotalGasUsed:        b0.Block.TotalGasUsed,
		},
		hash:              &blockHash,
		TransactionHashes: b0.TransactionHashes,
	}, nil
}
