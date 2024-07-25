package models

import (
	"fmt"
	"math/big"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
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

var GenesisBlock = &Block{Block: types.GenesisBlock}

func NewBlockFromBytes(data []byte) (*Block, error) {
	var b *Block
	err := rlp.DecodeBytes(data, &b)
	if err != nil {
		// todo temp remove this after previewnet is deprecated
		var bV0360 *blockV0360
		if err := rlp.DecodeBytes(data, &bV0360); err != nil {
			return nil, fmt.Errorf("failed to decode block [%x]: %w", data, err)
		}
		b = &Block{
			Block: &types.Block{
				ParentBlockHash:     bV0360.ParentBlockHash,
				Height:              bV0360.Height,
				Timestamp:           bV0360.Timestamp,
				TotalSupply:         bV0360.TotalSupply,
				ReceiptRoot:         bV0360.ReceiptRoot,
				TransactionHashRoot: gethCommon.Hash{},
				TotalGasUsed:        bV0360.TotalGasUsed,
			},
			TransactionHashes: bV0360.TransactionHashes,
		}
		h, err := bV0360.Hash()
		if err != nil {
			return nil, fmt.Errorf("failed to calculate backward compatible hash for block: %w", err)
		}
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

// TODO temp remove after previewnet is deprecated
// this is an older format for the block, that contains transaction hashes
type blockV0360 struct {
	// the hash of the parent block
	ParentBlockHash gethCommon.Hash
	// Height returns the height of this block
	Height uint64
	// Timestamp is a Unix timestamp in seconds at which the block was created
	// Note that this value must be provided from the FVM Block
	Timestamp uint64
	// holds the total amount of the native token deposited in the evm side. (in attoflow)
	TotalSupply *big.Int
	// ReceiptRoot returns the root hash of the receipts emitted in this block
	// Note that this value won't be unique to each block, for example for the
	// case of empty trie of receipts or a single receipt with no logs and failed state
	// the same receipt root would be reported for block.
	ReceiptRoot gethCommon.Hash

	// transaction hashes
	TransactionHashes []gethCommon.Hash

	// stores gas used by all transactions included in the block.
	TotalGasUsed uint64
}

func (b *blockV0360) Hash() (gethCommon.Hash, error) {
	data, err := rlp.EncodeToBytes(b)
	return gethCrypto.Keccak256Hash(data), err
}
