package api

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTransaction(t *testing.T) {
	validToAddress := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	zeroToAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")

	data, err := hex.DecodeString("c6888f")
	require.NoError(t, err)
	invalidTxData := hexutil.Bytes(data)

	data, err = hex.DecodeString("c6888fa1000000000000000000000000000000000000000000000000000000000000ab")
	require.NoError(t, err)
	invalidTxDataLen := hexutil.Bytes(data)

	gasLimit := uint64(125_000)

	nonce := uint64(1)
	tests := map[string]struct {
		txArgs TransactionArgs
		valid  bool
		errMsg string
	}{
		"valid transaction": {
			txArgs: TransactionArgs{
				Nonce:    (*hexutil.Uint64)(&nonce),
				To:       &validToAddress,
				Value:    (*hexutil.Big)(big.NewInt(0)),
				Gas:      (*hexutil.Uint64)(&gasLimit),
				GasPrice: (*hexutil.Big)(big.NewInt(0)),
				Data:     &hexutil.Bytes{},
			},
			valid:  true,
			errMsg: "",
		},
		"conflicting input and data": {
			txArgs: TransactionArgs{
				Nonce:    (*hexutil.Uint64)(&nonce),
				To:       &validToAddress,
				Value:    (*hexutil.Big)(big.NewInt(0)),
				Gas:      (*hexutil.Uint64)(&gasLimit),
				GasPrice: (*hexutil.Big)(big.NewInt(0)),
				Data:     &invalidTxDataLen,
				Input:    &invalidTxData,
			},
			valid:  false,
			errMsg: `ambiguous request: both "data" and "input" are set and are not identical`,
		},
		"send to 0 address": {
			txArgs: TransactionArgs{
				Nonce:    (*hexutil.Uint64)(&nonce),
				To:       &zeroToAddress,
				Value:    (*hexutil.Big)(big.NewInt(0)),
				Gas:      (*hexutil.Uint64)(&gasLimit),
				GasPrice: (*hexutil.Big)(big.NewInt(0)),
				Data:     &hexutil.Bytes{},
			},
			valid:  false,
			errMsg: "transaction recipient is the zero address",
		},
		"create empty contract (no value)": {
			txArgs: TransactionArgs{
				Nonce:    (*hexutil.Uint64)(&nonce),
				To:       nil,
				Value:    (*hexutil.Big)(big.NewInt(0)),
				Gas:      (*hexutil.Uint64)(&gasLimit),
				GasPrice: (*hexutil.Big)(big.NewInt(0)),
				Data:     &hexutil.Bytes{},
			},
			valid:  false,
			errMsg: "transaction will create a contract with empty code",
		},
		"create empty contract (nil value)": {
			txArgs: TransactionArgs{
				Nonce:    (*hexutil.Uint64)(&nonce),
				To:       nil,
				Value:    nil,
				Gas:      (*hexutil.Uint64)(&gasLimit),
				GasPrice: (*hexutil.Big)(big.NewInt(0)),
				Data:     &hexutil.Bytes{},
			},
			valid:  false,
			errMsg: "transaction will create a contract with empty code",
		},
		"create empty contract (with value)": {
			txArgs: TransactionArgs{
				Nonce:    (*hexutil.Uint64)(&nonce),
				To:       nil,
				Value:    (*hexutil.Big)(big.NewInt(150)),
				Gas:      (*hexutil.Uint64)(&gasLimit),
				GasPrice: (*hexutil.Big)(big.NewInt(0)),
				Data:     &hexutil.Bytes{},
			},
			valid:  false,
			errMsg: "transaction will create a contract with value but empty code",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.txArgs.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.errMsg)
			}
		})
	}

}
