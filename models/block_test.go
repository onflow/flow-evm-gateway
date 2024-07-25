package models

import (
	"math/big"
	"testing"

	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	flow2 "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DecodeBlockExecutedEvent(t *testing.T) {
	block := &types.Block{
		ParentBlockHash:     gethCommon.HexToHash("0x1"),
		Height:              100,
		TotalSupply:         big.NewInt(100),
		ReceiptRoot:         gethCommon.HexToHash("0x2"),
		TransactionHashRoot: gethCommon.HexToHash("0xf1"),
	}
	ev := events.NewBlockEvent(block)

	encEv, err := ev.Payload.ToCadence(flow2.Emulator)
	require.NoError(t, err)

	decBlock, err := decodeBlock(encEv)
	require.NoError(t, err)
	assert.Equal(t, block, decBlock)
}
