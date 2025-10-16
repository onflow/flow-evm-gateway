package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DecodeReceipts(t *testing.T) {
	cdcEv, rec := createTestEvent(t, evmTxBinary)

	_, receipt, _, err := DecodeTransactionEvent(cdcEv)
	require.NoError(t, err)

	assert.Equal(t, BaseFeePerGas, receipt.EffectiveGasPrice)

	for i, l := range rec.Logs {
		assert.ObjectsAreEqualValues(l, receipt.Logs[i])
		for j, tt := range l.Topics {
			assert.EqualValues(t, tt, receipt.Logs[i].Topics[j])
		}
	}
}
