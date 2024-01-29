package storage

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
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

func newReceipt(height uint64, ID common.Hash) *gethTypes.Receipt {
	txHash := common.HexToHash(fmt.Sprintf("0xff%d", height))
	return &gethTypes.Receipt{
		PostState:         common.Hash{2}.Bytes(),
		CumulativeGasUsed: 3,
		Logs: []*gethTypes.Log{
			{
				Address:     common.BytesToAddress([]byte{0x22}),
				Topics:      []common.Hash{common.HexToHash("alfa"), common.HexToHash("bravo")},
				BlockNumber: height,
				TxHash:      txHash,
				TxIndex:     1,
				BlockHash:   ID,
				Index:       2,
			},
			{
				Address:     common.BytesToAddress([]byte{0x02, 0x22}),
				Topics:      []common.Hash{common.HexToHash("charlie"), common.HexToHash("delta")},
				BlockNumber: height,
				TxHash:      txHash,
				TxIndex:     1,
				BlockHash:   ID,
				Index:       3,
			},
		},
		TxHash:            txHash,
		GasUsed:           2,
		EffectiveGasPrice: big.NewInt(22),
		BlockHash:         ID,
		BlockNumber:       big.NewInt(int64(height)),
		TransactionIndex:  1,
	}
}
