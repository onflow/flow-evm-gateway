package models

import (
	"bytes"
	"encoding/hex"
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

	tx := &gethTypes.Transaction{}
	txData, _ := hex.DecodeString("f9015880808301e8488080b901086060604052341561000f57600080fd5b60eb8061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063c6888fa1146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60007f24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da600783026040518082815260200191505060405180910390a16007820290509190505600a165627a7a7230582040383f19d9f65246752244189b02f56e8d0980ed44e7a56c0b200458caad20bb002982052fa09c05a7389284dc02b356ec7dee8a023c5efd3a9d844fa3c481882684b0640866a057e96d0a71a857ed509bb2b7333e78b2408574b8cc7f51238f25c58812662653")
	tx.UnmarshalBinary(txData)

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
	decTxHash, err := decTx.Hash()
	require.NoError(t, err)
	assert.Equal(t, tx.Hash(), decTxHash)
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
