package models

import (
	"math/big"
	"testing"

	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DecodeBlockExecutedEvent(t *testing.T) {
	gethBlock := &types.Block{
		ParentBlockHash:     gethCommon.HexToHash("0x1"),
		Height:              100,
		Timestamp:           1724406853,
		TotalSupply:         big.NewInt(100),
		ReceiptRoot:         gethCommon.HexToHash("0x2"),
		TransactionHashRoot: gethCommon.HexToHash("0x3"),
		TotalGasUsed:        uint64(30),
		PrevRandao:          gethCommon.HexToHash("0x15"),
	}
	block := &Block{Block: gethBlock}
	ev := events.NewBlockEvent(gethBlock)

	encEv, err := ev.Payload.ToCadence(flowGo.Previewnet)
	require.NoError(t, err)

	decBlock, err := decodeBlockEvent(encEv)
	require.NoError(t, err)

	assert.Equal(t, decBlock, block)
	h1, err := block.Hash()
	require.NoError(t, err)
	h2, err := decBlock.Hash()
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}

func Test_Hash(t *testing.T) {
	// we fix the hash calculation for bellow block, so we can detect
	// any breaking changes in hash calculation or block structure
	// coming from changes in EVM Core (flow-go), we should be aware of changes
	// and this test makes sure we are, if changes occur it means they break backward
	// compatibility when calculating hashes for older blocks.
	const hash = "0x1f0435edbc1600d96ae988eb580772b87b5a4b14c59c60e240182a492ac8fefe"

	gethBlock := &types.Block{
		ParentBlockHash:     gethCommon.HexToHash("0x1"),
		Height:              100,
		Timestamp:           1724406853,
		TotalSupply:         big.NewInt(100),
		ReceiptRoot:         gethCommon.HexToHash("0x2"),
		TransactionHashRoot: gethCommon.HexToHash("0x3"),
		TotalGasUsed:        uint64(30),
		PrevRandao:          gethCommon.HexToHash("0x15"),
	}
	block := &Block{Block: gethBlock}

	h, err := block.Hash()
	require.NoError(t, err)
	require.Equal(t, hash, h.String())
}

func Test_EncodingDecoding(t *testing.T) {
	block := &Block{
		Block: &types.Block{
			ParentBlockHash:     gethCommon.HexToHash("0x1"),
			Height:              100,
			Timestamp:           1724406853,
			TotalSupply:         big.NewInt(100),
			ReceiptRoot:         gethCommon.HexToHash("0x2"),
			TransactionHashRoot: gethCommon.HexToHash("0x3"),
			TotalGasUsed:        uint64(30),
			PrevRandao:          gethCommon.HexToHash("0x15"),
		},
		TransactionHashes: []gethCommon.Hash{
			gethCommon.HexToHash("0x55"),
			gethCommon.HexToHash("0x66"),
		},
	}

	bytes, err := block.ToBytes()
	require.NoError(t, err)

	blockDec, err := NewBlockFromBytes(bytes)
	require.NoError(t, err)

	assert.Equal(t, block, blockDec)
}
