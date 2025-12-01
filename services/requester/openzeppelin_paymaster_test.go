package requester

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/models"
)

func TestParseOpenZeppelinPaymasterData(t *testing.T) {
	t.Run("parses valid OpenZeppelin format", func(t *testing.T) {
		paymasterAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tokenAddr := common.HexToAddress("0x0987654321098765432109876543210987654321")
		validationData := []byte{0xaa, 0xbb, 0xcc}

		paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)
		paymasterAndData = append(paymasterAndData, validationData...)

		data, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		require.NoError(t, err)
		assert.Equal(t, paymasterAddr, data.PaymasterAddress)
		assert.Equal(t, tokenAddr, data.TokenAddress)
		assert.Equal(t, validationData, data.ValidationData)
	})

	t.Run("parses with empty validation data", func(t *testing.T) {
		paymasterAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tokenAddr := common.HexToAddress("0x0987654321098765432109876543210987654321")

		paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)

		data, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		require.NoError(t, err)
		assert.Equal(t, paymasterAddr, data.PaymasterAddress)
		assert.Equal(t, tokenAddr, data.TokenAddress)
		assert.Empty(t, data.ValidationData)
	})

	t.Run("rejects data too short", func(t *testing.T) {
		// Only 20 bytes (paymaster address), missing token address
		paymasterAndData := common.HexToAddress("0x1234567890123456789012345678901234567890").Bytes()

		_, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too short")
	})

	t.Run("rejects empty data", func(t *testing.T) {
		_, err := ParseOpenZeppelinPaymasterData([]byte{})
		assert.Error(t, err)
	})
}

func TestValidateOpenZeppelinPaymaster(t *testing.T) {
	t.Run("validates correct format", func(t *testing.T) {
		paymasterAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tokenAddr := common.HexToAddress("0x0987654321098765432109876543210987654321")

		paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)

		data, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		require.NoError(t, err)

		userOp := &models.UserOperation{
			Sender:               common.HexToAddress("0x1111111111111111111111111111111111111111"),
			Nonce:                big.NewInt(0),
			CallData:             []byte{},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     paymasterAndData,
			Signature:            []byte{},
		}

		err = ValidateOpenZeppelinPaymaster(userOp, data, zerolog.Nop())
		assert.NoError(t, err)
	})

	t.Run("rejects zero paymaster address", func(t *testing.T) {
		paymasterAddr := common.Address{} // Zero address
		tokenAddr := common.HexToAddress("0x0987654321098765432109876543210987654321")

		paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)

		data, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		require.NoError(t, err)

		userOp := &models.UserOperation{
			PaymasterAndData: paymasterAndData,
		}

		err = ValidateOpenZeppelinPaymaster(userOp, data, zerolog.Nop())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid paymaster address")
	})

	t.Run("rejects zero token address", func(t *testing.T) {
		paymasterAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tokenAddr := common.Address{} // Zero address

		paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)

		data, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		require.NoError(t, err)

		userOp := &models.UserOperation{
			PaymasterAndData: paymasterAndData,
		}

		err = ValidateOpenZeppelinPaymaster(userOp, data, zerolog.Nop())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid token address")
	})
}

func TestEstimateOpenZeppelinPaymasterCost(t *testing.T) {
	t.Run("estimates cost correctly", func(t *testing.T) {
		paymasterAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tokenAddr := common.HexToAddress("0x0987654321098765432109876543210987654321")

		paymasterAndData := append(paymasterAddr.Bytes(), tokenAddr.Bytes()...)

		data, err := ParseOpenZeppelinPaymasterData(paymasterAndData)
		require.NoError(t, err)

		userOp := &models.UserOperation{
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(200000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000), // 1 gwei
		}

		cost := EstimateOpenZeppelinPaymasterCost(userOp, data)
		assert.NotNil(t, cost)
		assert.True(t, cost.Sign() > 0)

		// Expected: 1000000000 * (100000 + 200000 + 50000) = 350000000000000
		expected := big.NewInt(350000000000000)
		assert.Equal(t, expected, cost)
	})
}

