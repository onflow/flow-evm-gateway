package mocks

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
	"golang.org/x/exp/rand"

	"github.com/onflow/flow-evm-gateway/models"
)

func NewBlock(height uint64) *models.Block {
	parent := common.HexToHash(fmt.Sprintf("0x0%d", height-1))
	if height == 0 {
		parent = common.Hash{}
	}

	return &models.Block{
		Block: &types.Block{
			ParentBlockHash:     parent,
			Height:              height,
			Timestamp:           uint64(time.Now().Second()),
			TotalSupply:         big.NewInt(1000),
			ReceiptRoot:         common.HexToHash(fmt.Sprintf("0x100%d", height)),
			TransactionHashRoot: common.HexToHash(fmt.Sprintf("0x200%d", height)),
			TotalGasUsed:        uint64(30_000),
			PrevRandao:          common.HexToHash(fmt.Sprintf("0x300%d", height)),
		},
		TransactionHashes: make([]common.Hash, 0),
	}
}

// NewBlockWithParent creates a new block at the given height with the parent hash
// correctly set to the hash of the provided parent block.
func NewBlockWithParent(height uint64, parent *models.Block) *models.Block {
	var parentHash common.Hash
	if parent != nil {
		parentHash, _ = parent.Hash()
	}

	return &models.Block{
		Block: &types.Block{
			ParentBlockHash:     parentHash,
			Height:              height,
			Timestamp:           uint64(time.Now().Second()),
			TotalSupply:         big.NewInt(1000),
			ReceiptRoot:         common.HexToHash(fmt.Sprintf("0x100%d", height)),
			TransactionHashRoot: common.HexToHash(fmt.Sprintf("0x200%d", height)),
			TotalGasUsed:        uint64(30_000),
			PrevRandao:          common.HexToHash(fmt.Sprintf("0x300%d", height)),
		},
		TransactionHashes: make([]common.Hash, 0),
	}
}

func NewReceipt(block *models.Block) *models.Receipt {
	txHash := common.HexToHash(fmt.Sprintf("0xff%d", block.Height))
	blockHash, _ := block.Hash()

	return &models.Receipt{
		PostState:         common.Hash{2}.Bytes(),
		CumulativeGasUsed: 3,
		Logs: []*gethTypes.Log{
			{
				Address:        common.BytesToAddress([]byte{0x22}),
				Topics:         []common.Hash{common.HexToHash("alfa"), common.HexToHash("bravo")},
				BlockNumber:    block.Height,
				TxHash:         txHash,
				TxIndex:        1,
				BlockHash:      blockHash,
				BlockTimestamp: block.Timestamp,
				Index:          0,
				Data:           fmt.Appendf(nil, "data-1%d", block.Height),
			},
			{
				Address:        common.BytesToAddress([]byte{0x02, 0x22}),
				Topics:         []common.Hash{common.HexToHash("charlie"), common.HexToHash("delta")},
				BlockNumber:    block.Height,
				TxHash:         txHash,
				TxIndex:        1,
				BlockHash:      blockHash,
				BlockTimestamp: block.Timestamp,
				Index:          1,
				Data:           fmt.Appendf(nil, "data-2%d", block.Height),
			},
		},
		TxHash:            txHash,
		GasUsed:           2,
		EffectiveGasPrice: big.NewInt(22),
		BlockHash:         blockHash,
		BlockNumber:       big.NewInt(int64(block.Height)),
		TransactionIndex:  1,
		Bloom:             gethTypes.Bloom{byte(block.Height), byte(rand.Int())},
	}
}

func NewTransaction(nonce uint64) models.Transaction {
	return models.TransactionCall{
		Transaction: gethTypes.NewTx(&gethTypes.DynamicFeeTx{
			ChainID:   types.FlowEVMPreviewNetChainID,
			Nonce:     nonce,
			To:        &common.Address{0x01, 0x02},
			Gas:       123457,
			GasFeeCap: big.NewInt(13),
			GasTipCap: big.NewInt(0),
			Data:      []byte{},
		}),
	}
}
