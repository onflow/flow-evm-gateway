package models

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTransaction(t *testing.T) {
	validToAddress := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	zeroToAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")

	smallContractPayload, err := hex.DecodeString("c6888fa1")
	require.NoError(t, err)

	invalidTxData, err := hex.DecodeString("c6888f")
	require.NoError(t, err)

	invalidTxDataLen, err := hex.DecodeString("c6888fa1000000000000000000000000000000000000000000000000000000000000ab")
	require.NoError(t, err)

	tests := map[string]struct {
		tx     *types.Transaction
		valid  bool
		errMsg string
	}{
		"valid transaction": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       &validToAddress,
					Value:    big.NewInt(0),
					Gas:      25_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  true,
			errMsg: "",
		},
		"send to 0 address": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       &zeroToAddress,
					Value:    big.NewInt(0),
					Gas:      25_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  false,
			errMsg: "transaction recipient is the zero address",
		},
		"create empty contract (no value)": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(0),
					Gas:      53_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  false,
			errMsg: "transaction will create a contract with empty code",
		},
		"create empty contract (with value)": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(150),
					Gas:      53_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  false,
			errMsg: "transaction will create a contract with value but empty code",
		},
		"small payload for create": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(150),
					Gas:      53_000,
					GasPrice: big.NewInt(0),
					Data:     smallContractPayload,
				},
			),
			valid:  false,
			errMsg: "transaction will create a contract, but the payload is suspiciously small (4 bytes)",
		},
		"tx data length < 4": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       &validToAddress,
					Value:    big.NewInt(150),
					Gas:      153_000,
					GasPrice: big.NewInt(0),
					Data:     invalidTxData,
				},
			),
			valid:  false,
			errMsg: "transaction data is not valid ABI (missing the 4 byte call prefix)",
		},
		"tx data (excluding function selector) not divisible by 32": {
			tx: types.NewTx(
				&types.LegacyTx{
					Nonce:    1,
					To:       &validToAddress,
					Value:    big.NewInt(150),
					Gas:      153_000,
					GasPrice: big.NewInt(0),
					Data:     invalidTxDataLen,
				},
			),
			valid:  false,
			errMsg: "transaction data is not valid ABI (length should be a multiple of 32 (was 31))",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateTransaction(tc.tx)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.errMsg)
			}
		})
	}

}
