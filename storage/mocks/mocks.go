package mocks

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
)

func NewBlock(height uint64) *types.Block {
	parent := common.HexToHash(fmt.Sprintf("0x0%d", height-1))
	if height == 0 {
		parent = common.Hash{}
	}

	return &types.Block{
		ParentBlockHash:   parent,
		Height:            height,
		TotalSupply:       big.NewInt(1000),
		ReceiptRoot:       common.HexToHash(fmt.Sprintf("0x1337%d", height)),
		TransactionHashes: make([]common.Hash, 0),
	}
}

func NewReceipt(height uint64, ID common.Hash) *gethTypes.Receipt {
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
				Index:       0,
				Data:        []byte(fmt.Sprintf("data-1%d", height)),
			},
			{
				Address:     common.BytesToAddress([]byte{0x02, 0x22}),
				Topics:      []common.Hash{common.HexToHash("charlie"), common.HexToHash("delta")},
				BlockNumber: height,
				TxHash:      txHash,
				TxIndex:     1,
				BlockHash:   ID,
				Index:       1,
				Data:        []byte(fmt.Sprintf("data-2%d", height)),
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

func NewTransaction(nonce uint64) *gethTypes.Transaction {
	return gethTypes.NewTx(&gethTypes.DynamicFeeTx{
		ChainID:   types.FlowEVMTestnetChainID,
		Nonce:     nonce,
		To:        &common.Address{0x01, 0x02},
		Gas:       123457,
		GasFeeCap: big.NewInt(13),
		GasTipCap: big.NewInt(0),
		Data:      []byte{},
	})
}
