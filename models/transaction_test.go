package models

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestEvent(t *testing.T) (cadence.Event, *gethTypes.Transaction, *types.Result) {
	res := &types.Result{
		VMError:                 nil,
		TxType:                  1,
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

	tx := gethTypes.NewTx(&gethTypes.AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    1,
		GasPrice: big.NewInt(24),
		Gas:      1337,
		To:       &common.Address{0x01},
		Value:    big.NewInt(5),
		Data:     []byte{0x2, 0x3},
	})

	var txEnc bytes.Buffer
	err := tx.EncodeRLP(&txEnc)
	require.NoError(t, err)

	ev := types.NewTransactionExecutedEvent(
		1,
		txEnc.Bytes(),
		common.HexToHash("0x1"),
		tx.Hash(),
		res,
	)

	cdcEv, err := ev.Payload.CadenceEvent()
	require.NoError(t, err)

	return cdcEv, tx, res
}

func Test_DecodeTransaction(t *testing.T) {
	cdcEv, tx, _ := createTestEvent(t)

	decTx, err := DecodeTransaction(cdcEv)
	require.NoError(t, err)
	assert.Equal(t, tx.Hash(), decTx.Hash())
	assert.Equal(t, tx.Type(), decTx.Type())
	assert.Equal(t, tx.To(), decTx.To())
	assert.Equal(t, tx.Value(), decTx.Value())
	assert.Equal(t, tx.Nonce(), decTx.Nonce())
	assert.Equal(t, tx.Data(), decTx.Data())
	assert.Equal(t, tx.GasPrice(), decTx.GasPrice())
	assert.Equal(t, tx.BlobGas(), decTx.BlobGas())
	assert.Equal(t, tx.Size(), decTx.Size())
}

func Test_DecodeReceipts(t *testing.T) {
	cdcEv, _, rec := createTestEvent(t)

	receipt, err := DecodeReceipt(cdcEv)
	require.NoError(t, err)

	for i, l := range rec.Logs {
		assert.ObjectsAreEqualValues(l, receipt.Logs[i])
		for j, tt := range l.Topics {
			assert.EqualValues(t, tt, receipt.Logs[i].Topics[j])
		}
	}
}
