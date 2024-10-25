package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DecodeReceipts(t *testing.T) {
	cdcEv, rec := createTestEvent(t, evmTxBinary)

	_, receipt, _, err := decodeTransactionEvent(cdcEv)
	require.NoError(t, err)

	for i, l := range rec.Logs {
		assert.ObjectsAreEqualValues(l, receipt.Logs[i])
		for j, tt := range l.Topics {
			assert.EqualValues(t, tt, receipt.Logs[i].Topics[j])
		}
	}
}
