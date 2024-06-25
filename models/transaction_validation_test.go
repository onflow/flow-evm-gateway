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
	t.Parallel()

	t.Run("valid transaction", func(t *testing.T) {
		t.Parallel()

		to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       &to,
				Value:    big.NewInt(0),
				Gas:      25_000,
				GasPrice: big.NewInt(0),
				Data:     []byte{},
			},
		)

		err := ValidateTransaction(tx)
		assert.NoError(t, err)
	})

	t.Run("send to 0 address", func(t *testing.T) {
		t.Parallel()

		to := common.HexToAddress("0x0000000000000000000000000000000000000000")
		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       &to,
				Value:    big.NewInt(0),
				Gas:      25_000,
				GasPrice: big.NewInt(0),
				Data:     []byte{},
			},
		)

		err := ValidateTransaction(tx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "transaction recipient is the zero address")
	})

	t.Run("create empty contract (no value)", func(t *testing.T) {
		t.Parallel()

		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       nil,
				Value:    big.NewInt(0),
				Gas:      53_000,
				GasPrice: big.NewInt(0),
				Data:     []byte{},
			},
		)

		err := ValidateTransaction(tx)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction will create a contract with empty code",
		)
	})

	t.Run("create empty contract (with value)", func(t *testing.T) {
		t.Parallel()

		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       nil,
				Value:    big.NewInt(150),
				Gas:      53_000,
				GasPrice: big.NewInt(0),
				Data:     []byte{},
			},
		)

		err := ValidateTransaction(tx)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction will create a contract with value but empty code",
		)
	})

	t.Run("small payload for create", func(t *testing.T) {
		t.Parallel()

		data, err := hex.DecodeString("c6888fa1")
		require.NoError(t, err)

		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       nil,
				Value:    big.NewInt(150),
				Gas:      53_000,
				GasPrice: big.NewInt(0),
				Data:     data,
			},
		)

		err = ValidateTransaction(tx)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction will create a contract, but the payload is suspiciously small (4 bytes)",
		)
	})

	t.Run("tx data length < 4", func(t *testing.T) {
		t.Parallel()

		data, err := hex.DecodeString("c6888f")
		require.NoError(t, err)

		to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       &to,
				Value:    big.NewInt(150),
				Gas:      153_000,
				GasPrice: big.NewInt(0),
				Data:     data,
			},
		)

		err = ValidateTransaction(tx)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction data is not valid ABI (missing the 4 byte call prefix)",
		)
	})

	t.Run("tx data (excluding function selector) not divisible by 32", func(t *testing.T) {
		t.Parallel()

		data, err := hex.DecodeString("c6888fa1000000000000000000000000000000000000000000000000000000000000ab")
		require.NoError(t, err)

		to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
		tx := types.NewTx(
			&types.LegacyTx{
				Nonce:    1,
				To:       &to,
				Value:    big.NewInt(150),
				Gas:      153_000,
				GasPrice: big.NewInt(0),
				Data:     data,
			},
		)

		err = ValidateTransaction(tx)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction data is not valid ABI (length should be a multiple of 32 (was 31))",
		)
	})
}
