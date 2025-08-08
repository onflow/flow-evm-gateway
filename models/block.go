package models

import (
	"fmt"
	"math/big"

	gethCommon "github.com/ethereum/go-ethereum/common"
	gethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
)

var (
	EarliestBlockNumber  = big.NewInt(-5)
	SafeBlockNumber      = big.NewInt(-4)
	FinalizedBlockNumber = big.NewInt(-3)
	LatestBlockNumber    = big.NewInt(-2)
	PendingBlockNumber   = big.NewInt(-1)
	zeroGethHash         = gethCommon.HexToHash("0x0")
)

func GenesisBlock(chainID flow.ChainID) *Block {
	return &Block{
		Block:             types.GenesisBlock(chainID),
		TransactionHashes: []gethCommon.Hash{},
	}
}

func NewBlockFromBytes(data []byte) (*Block, error) {
	var b *Block
	if err := rlp.DecodeBytes(data, &b); err != nil {
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
	// We define fixed hash in case where types.Block format changes which
	// will produce a different hash on Block.Hash() calculation since it
	// will have more fields than before, so we make sure the hash we calculated
	// with the previous format is fixed by assigning it to this field and then
	// on hash calculation we check if this field is set we just return it.
	// We must make the FixedHash exported so RLP encoding preserves it.
	FixedHash         gethCommon.Hash
	TransactionHashes []gethCommon.Hash
}

func (b *Block) ToBytes() ([]byte, error) {
	return rlp.EncodeToBytes(b)
}

func (b *Block) Hash() (gethCommon.Hash, error) {
	if b.FixedHash != zeroGethHash {
		return b.FixedHash, nil
	}
	return b.Block.Hash()
}

// decodeBlockEvent takes a cadence event that contains executed block payload and
// decodes it into the Block type.
func decodeBlockEvent(event cadence.Event) (*Block, *events.BlockEventPayload, error) {
	payload, err := events.DecodeBlockEventPayload(event)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to Cadence-decode EVM block event [%s]: %w",
			event.String(),
			err,
		)
	}

	fixedHash := gethCommon.Hash{}
	// If the `PrevRandao` field is the zero hash, we know that
	// this is a block with the legacy format, and we need to
	// fix its hash, due to the hash calculation breaking change.
	if payload.PrevRandao == zeroGethHash {
		fixedHash = payload.Hash
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
		FixedHash: fixedHash,
	}, payload, nil
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
	if err != nil {
		return gethCommon.Hash{}, err
	}
	return gethCrypto.Keccak256Hash(data), nil
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
// previous versions of block type. If it succeeds it will return the
// migrated block, otherwise it will return the decoding error.
func decodeBlockBreakingChanges(encoded []byte) (*Block, error) {
	b0 := &blockV0{}
	if err := rlp.DecodeBytes(encoded, b0); err != nil {
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
		FixedHash:         blockHash,
		TransactionHashes: b0.TransactionHashes,
	}, nil
}
