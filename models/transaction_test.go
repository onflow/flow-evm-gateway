package models

import (
	"bytes"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

func Test_DecodeTransaction(t *testing.T) {
	res := &types.Result{
		Failed:                  false,
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

	var txEnc []byte
	err := tx.EncodeRLP(bytes.NewBuffer(txEnc))
	require.NoError(t, err)

	ev := types.NewTransactionExecutedEvent(1, txEnc, tx.Hash(), res)

	cdcEv, err := ev.Payload.CadenceEvent()
	require.NoError(t, err)

	decTx, err := DecodeTransaction(cdcEv)
	require.NoError(t, err)
	assert.Equal(t, tx, decTx)
}
