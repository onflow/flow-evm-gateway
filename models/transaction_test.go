package models

import (
	_ "embed"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed fixtures/transaction_call.bin
var evmTxBinary string

//go:embed fixtures/direct_call.bin
var directCallBinary string

func createTestEvent(t *testing.T, txBinary string) (cadence.Event, *types.Result) {
	txEncoded, err := hex.DecodeString(txBinary)
	require.NoError(t, err)

	var txHash common.Hash
	var txType uint8
	if txEncoded[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(txEncoded)
		require.NoError(t, err)

		txHash, err = directCall.Hash()
		require.NoError(t, err)

		txType = types.DirectCallTxType
	} else {
		gethTx := &gethTypes.Transaction{}
		gethTx.UnmarshalBinary(txEncoded)
		txHash = gethTx.Hash()
		txType = gethTx.Type()
	}

	res := &types.Result{
		VMError:                 nil,
		TxType:                  txType,
		GasConsumed:             1337,
		DeployedContractAddress: types.Address{0x5, 0x6, 0x7},
		ReturnedValue:           []byte{0x55},
		Logs: []*gethTypes.Log{{
			Address: common.Address{0x1, 0x2},
			Topics:  []common.Hash{{0x5, 0x6}, {0x7, 0x8}},
		}, {
			Address: common.Address{0x3, 0x5},
			Topics:  []common.Hash{{0x2, 0x66}, {0x7, 0x1}},
		}},
	}

	ev := types.NewTransactionExecutedEvent(
		1,
		txEncoded,
		common.HexToHash("0x1"),
		txHash,
		res,
	)

	cdcEv, err := ev.Payload.CadenceEvent()
	require.NoError(t, err)

	return cdcEv, res
}

func Test_DecodeEVMTransaction(t *testing.T) {
	cdcEv, _ := createTestEvent(t, evmTxBinary)

	decTx, err := decodeTransaction(cdcEv)
	require.NoError(t, err)
	require.IsType(t, TransactionCall{}, decTx)

	txHash, err := decTx.Hash()
	require.NoError(t, err)
	v, r, s := decTx.RawSignatureValues()
	from, err := decTx.From()
	require.NoError(t, err)

	assert.Equal(
		t,
		common.HexToHash("0xe414f90fea2aebd75e8c0b3b6a4a0c9928e86c16ea724343d884f40bfe2c4c6b"),
		txHash,
	)
	assert.Equal(t, big.NewInt(1327), v)
	assert.Equal(
		t,
		"70570792731140625669797097963380383355394381124817754245181559778290488838246",
		r.String(),
	)
	assert.Equal(
		t,
		"39763645764758347623445260367025516531172546351546206339075417954057005180499",
		s.String(),
	)
	assert.Equal(
		t,
		common.HexToAddress("0x658Bdf435d810C91414eC09147DAA6DB62406379"),
		from,
	)
	assert.Nil(t, decTx.To())
	assert.Len(t, decTx.Data(), 264)
	assert.Equal(t, uint64(0), decTx.Nonce())
	assert.Equal(t, big.NewInt(0), decTx.Value())
	assert.Equal(t, uint8(0), decTx.Type())
	assert.Equal(t, uint64(125_000), decTx.Gas())
	assert.Equal(t, big.NewInt(0), decTx.GasPrice())
	assert.Equal(t, uint64(0), decTx.BlobGas())
	assert.Equal(t, uint64(347), decTx.Size())
}

func Test_DecodeDirectCall(t *testing.T) {
	cdcEv, _ := createTestEvent(t, directCallBinary)

	decTx, err := decodeTransaction(cdcEv)
	require.NoError(t, err)
	require.IsType(t, DirectCall{}, decTx)

	txHash, err := decTx.Hash()
	require.NoError(t, err)
	v, r, s := decTx.RawSignatureValues()
	from, err := decTx.From()
	require.NoError(t, err)

	assert.Equal(
		t,
		common.HexToHash("0xe090f3a66f269d436e4185551d790d923f53a2caabf475c18d60bf1f091813d9"),
		txHash,
	)
	assert.Equal(t, big.NewInt(0), v)
	assert.Equal(t, big.NewInt(0), r)
	assert.Equal(t, big.NewInt(0), s)
	assert.Equal(
		t,
		common.HexToAddress("0x0000000000000000000000010000000000000000"),
		from,
	)
	assert.Equal(
		t,
		common.HexToAddress("0x000000000000000000000002ef6737ccBbAa9977"),
		*decTx.To(),
	)
	assert.Empty(t, decTx.Data())
	assert.Equal(t, uint64(0), decTx.Nonce())
	assert.Equal(t, big.NewInt(10000000000), decTx.Value())
	assert.Equal(t, types.DirectCallTxType, decTx.Type())
	assert.Equal(t, uint64(23_300), decTx.Gas())
	assert.Equal(t, big.NewInt(0), decTx.GasPrice())
	assert.Equal(t, uint64(0), decTx.BlobGas())
	assert.Equal(t, uint64(59), decTx.Size())
}

func Test_UnmarshalTransaction(t *testing.T) {
	t.Parallel()

	t.Run("with TransactionCall value", func(t *testing.T) {
		t.Parallel()

		cdcEv, _ := createTestEvent(t, evmTxBinary)

		tx, err := decodeTransaction(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		decTx, err := UnmarshalTransaction(encodedTx)
		require.NoError(t, err)
		require.IsType(t, TransactionCall{}, decTx)

		txHash, err := decTx.Hash()
		require.NoError(t, err)
		v, r, s := decTx.RawSignatureValues()
		from, err := decTx.From()
		require.NoError(t, err)

		assert.Equal(
			t,
			common.HexToHash("0xe414f90fea2aebd75e8c0b3b6a4a0c9928e86c16ea724343d884f40bfe2c4c6b"),
			txHash,
		)
		assert.Equal(t, big.NewInt(1327), v)
		assert.Equal(
			t,
			"70570792731140625669797097963380383355394381124817754245181559778290488838246",
			r.String(),
		)
		assert.Equal(
			t,
			"39763645764758347623445260367025516531172546351546206339075417954057005180499",
			s.String(),
		)
		assert.Equal(
			t,
			common.HexToAddress("0x658Bdf435d810C91414eC09147DAA6DB62406379"),
			from,
		)
		assert.Nil(t, decTx.To())
		assert.Len(t, decTx.Data(), 264)
		assert.Equal(t, uint64(0), decTx.Nonce())
		assert.Equal(t, big.NewInt(0), decTx.Value())
		assert.Equal(t, uint8(0), decTx.Type())
		assert.Equal(t, uint64(125_000), decTx.Gas())
		assert.Equal(t, big.NewInt(0), decTx.GasPrice())
		assert.Equal(t, uint64(0), decTx.BlobGas())
		assert.Equal(t, uint64(347), decTx.Size())
	})

	t.Run("with DirectCall value", func(t *testing.T) {
		t.Parallel()

		cdcEv, _ := createTestEvent(t, directCallBinary)

		tx, err := decodeTransaction(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		decTx, err := UnmarshalTransaction(encodedTx)
		require.NoError(t, err)
		require.IsType(t, DirectCall{}, decTx)

		txHash, err := decTx.Hash()
		require.NoError(t, err)
		v, r, s := decTx.RawSignatureValues()
		from, err := decTx.From()
		require.NoError(t, err)

		assert.Equal(
			t,
			common.HexToHash("0xe090f3a66f269d436e4185551d790d923f53a2caabf475c18d60bf1f091813d9"),
			txHash,
		)
		assert.Equal(t, big.NewInt(0), v)
		assert.Equal(t, big.NewInt(0), r)
		assert.Equal(t, big.NewInt(0), s)
		assert.Equal(
			t,
			common.HexToAddress("0x0000000000000000000000010000000000000000"),
			from,
		)
		assert.Equal(
			t,
			common.HexToAddress("0x000000000000000000000002ef6737ccBbAa9977"),
			*decTx.To(),
		)
		assert.Empty(t, decTx.Data())
		assert.Equal(t, uint64(0), decTx.Nonce())
		assert.Equal(t, big.NewInt(10000000000), decTx.Value())
		assert.Equal(t, types.DirectCallTxType, decTx.Type())
		assert.Equal(t, uint64(23_300), decTx.Gas())
		assert.Equal(t, big.NewInt(0), decTx.GasPrice())
		assert.Equal(t, uint64(0), decTx.BlobGas())
		assert.Equal(t, uint64(59), decTx.Size())
	})
}
