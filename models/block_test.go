package models

import (
	"math/big"
	"testing"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/stdlib"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DecodePastBlockFormat(t *testing.T) {
	blockBytes := []byte{
		248, 156, 248, 120, 160, 5, 170, 74, 110, 219, 207, 111, 168, 17,
		120, 86, 101, 150, 190, 28, 127, 255, 123, 114, 22, 21, 200, 179,
		187, 209, 79, 247, 109, 156, 129, 236, 155, 130, 9, 215, 132, 102,
		189, 0, 250, 136, 13, 224, 182, 181, 251, 111, 228, 0, 160, 57, 163,
		66, 17, 141, 15, 78, 47, 246, 121, 13, 19, 7, 126, 229, 201, 221, 85,
		213, 225, 164, 193, 181, 63, 108, 177, 251, 179, 104, 191, 249, 104,
		160, 182, 119, 229, 18, 214, 49, 27, 157, 131, 54, 121, 174, 222, 128,
		0, 55, 18, 11, 170, 131, 254, 16, 83, 211, 50, 143, 168, 121, 144, 218,
		234, 244, 131, 18, 177, 169, 225, 160, 86, 232, 217, 88, 110, 212, 66,
		33, 54, 171, 14, 121, 201, 197, 55, 179, 63, 9, 183, 163, 184, 112, 202,
		86, 158, 35, 10, 21, 185, 37, 195, 178,
	}

	block, err := NewBlockFromBytes(blockBytes)
	require.NoError(t, err)

	blockHash, err := block.Hash()
	require.NoError(t, err)

	assert.Equal(
		t,
		gethCommon.HexToHash("0xcad79e3019da8014f623f351f01c88d1bcb4613352d4801548c6b07992fd1393"),
		blockHash,
	)
	assert.Equal(
		t,
		gethCommon.HexToHash("0x05aa4a6edbcf6fa81178566596be1c7fff7b721615c8b3bbd14ff76d9c81ec9b"),
		block.ParentBlockHash,
	)
	assert.Equal(t, uint64(2519), block.Height)
	assert.Equal(t, uint64(1723662586), block.Timestamp)
	assert.Equal(t, big.NewInt(1000000010000000000), block.TotalSupply)
	assert.Equal(
		t,
		gethCommon.HexToHash("0x39a342118d0f4e2ff6790d13077ee5c9dd55d5e1a4c1b53f6cb1fbb368bff968"),
		block.ReceiptRoot,
	)
	assert.Equal(
		t,
		gethCommon.HexToHash("0xb677e512d6311b9d833679aede800037120baa83fe1053d3328fa87990daeaf4"),
		block.TransactionHashRoot,
	)
	assert.Equal(t, uint64(1_225_129), block.TotalGasUsed)
	assert.Equal(
		t,
		gethCommon.Hash{},
		block.PrevRandao,
	)
	assert.Equal(
		t,
		[]gethCommon.Hash{
			gethCommon.HexToHash("0x56e8d9586ed4422136ab0e79c9c537b33f09b7a3b870ca569e230a15b925c3b2"),
		},
		block.TransactionHashes,
	)
}

func Test_FixedHashBlock(t *testing.T) {
	fixed := gethCommon.HexToHash("0x2")
	block := Block{
		Block: &types.Block{
			Height: 1,
		},
		FixedHash: fixed,
		TransactionHashes: []gethCommon.Hash{
			gethCommon.HexToHash("0x3"),
			gethCommon.HexToHash("0x4"),
		},
	}

	h, err := block.Hash()
	require.NoError(t, err)
	assert.Equal(t, fixed, h)

	data, err := block.ToBytes()
	require.NoError(t, err)

	decoded, err := NewBlockFromBytes(data)
	require.NoError(t, err)

	// make sure fixed hash and transaction hashes persists after decoding
	h, err = decoded.Hash()
	require.NoError(t, err)
	require.Equal(t, fixed, h)
	require.Equal(t, block.TransactionHashes, decoded.TransactionHashes)
}

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

	decBlock, _, err := decodeBlockEvent(encEv)
	require.NoError(t, err)

	assert.Equal(t, decBlock, block)
	h1, err := block.Hash()
	require.NoError(t, err)
	h2, err := decBlock.Hash()
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}

func Test_DecodingLegacyBlockExecutedEvent(t *testing.T) {
	block := types.Block{
		ParentBlockHash:     gethCommon.HexToHash("0x2"),
		Height:              1,
		Timestamp:           123,
		TotalSupply:         big.NewInt(222),
		ReceiptRoot:         gethCommon.HexToHash("0x4"),
		TransactionHashRoot: gethCommon.HexToHash("0x5"),
		TotalGasUsed:        100,
	}

	blockHash, err := block.Hash()
	require.NoError(t, err)

	eventType := stdlib.CadenceTypesForChain(flowGo.Testnet).BlockExecuted

	legacyEvent := cadence.NewEvent([]cadence.Value{
		cadence.NewUInt64(block.Height),
		hashToCadenceArrayValue(blockHash),
		cadence.NewUInt64(block.Timestamp),
		cadence.NewIntFromBig(block.TotalSupply),
		cadence.NewUInt64(block.TotalGasUsed),
		hashToCadenceArrayValue(block.ParentBlockHash),
		hashToCadenceArrayValue(block.ReceiptRoot),
		hashToCadenceArrayValue(block.TransactionHashRoot),
	}).WithType(eventType)

	b, _, err := decodeBlockEvent(legacyEvent)
	require.NoError(t, err)

	require.Equal(t, block.ParentBlockHash, b.ParentBlockHash)
	require.Equal(t, block.Height, b.Height)
	bh, err := block.Hash()
	require.NoError(t, err)
	dech, err := b.Hash()
	require.NoError(t, err)
	require.Equal(t, bh, dech)

	require.NotNil(t, b.FixedHash)
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
	require.Equal(t, block, blockDec)

	dh, err := blockDec.Hash()
	require.NoError(t, err)
	bh, err := block.Hash()
	require.NoError(t, err)
	require.Equal(t, bh, dh)
}

func hashToCadenceArrayValue(hash gethCommon.Hash) cadence.Array {
	values := make([]cadence.Value, len(hash))
	for i, v := range hash {
		values[i] = cadence.NewUInt8(v)
	}
	return cadence.NewArray(values).
		WithType(cadenceHashType)
}

var cadenceHashType = cadence.NewConstantSizedArrayType(gethCommon.HashLength, cadence.UInt8Type)
