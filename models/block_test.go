package models

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

func Test_DecodeBlockExecutedEvent(t *testing.T) {
	block := &types.Block{
		ParentBlockHash: common.HexToHash("0x1"),
		Height:          100,
		TotalSupply:     big.NewInt(100),
		ReceiptRoot:     common.HexToHash("0x2"),
		TransactionHashes: []common.Hash{
			common.HexToHash("0xf1"),
		},
	}
	ev := types.NewBlockExecutedEvent(block)

	encEv, err := ev.Payload.CadenceEvent()
	require.NoError(t, err)

	decBlock, err := DecodeBlock(encEv)
	require.NoError(t, err)
	assert.Equal(t, block, decBlock)
}
