package models

import (
	"math/big"
	"testing"

	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DecodeBlockExecutedEvent(t *testing.T) {
	block := &types.Block{
		ParentBlockHash: gethCommon.HexToHash("0x1"),
		Height:          100,
		TotalSupply:     big.NewInt(100),
		ReceiptRoot:     gethCommon.HexToHash("0x2"),
		TransactionHashes: []gethCommon.Hash{
			gethCommon.HexToHash("0xf1"),
		},
	}
	ev := types.NewBlockEvent(block)

	location := common.NewAddressLocation(nil, common.Address{0x1}, string(types.EventTypeBlockExecuted))
	encEv, err := ev.Payload.ToCadence(location)
	require.NoError(t, err)

	decBlock, err := decodeBlockEvent(encEv)
	require.NoError(t, err)
	assert.Equal(t, block, decBlock)
}
