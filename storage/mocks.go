package storage

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm/types"
	"math/big"
)

func newBlock(height uint64) *types.Block {
	parent := common.HexToHash(fmt.Sprintf("0x0%d", height-1))
	if height == 0 {
		parent = common.Hash{}
	}

	return &types.Block{
		ParentBlockHash:   parent,
		Height:            height,
		TotalSupply:       big.NewInt(1000),
		ReceiptRoot:       common.HexToHash(fmt.Sprintf("0x1337%d", height)),
		TransactionHashes: nil,
	}
}
