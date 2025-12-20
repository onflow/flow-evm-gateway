package requester

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
)

// OpenZeppelinPaymasterData represents the decoded paymasterAndData for OpenZeppelin PaymasterERC20
// Format: paymasterAddress (20 bytes) + tokenAddress (20 bytes) + [validation data]
type OpenZeppelinPaymasterData struct {
	PaymasterAddress common.Address
	TokenAddress     common.Address
	ValidationData   []byte
}

// ParseOpenZeppelinPaymasterData parses paymasterAndData for OpenZeppelin PaymasterERC20 contracts
// OpenZeppelin PaymasterERC20 format:
// - paymasterAddress: 20 bytes
// - tokenAddress: 20 bytes (for PaymasterERC20)
// - validationData: variable length (token price, exchange rate, etc.)
func ParseOpenZeppelinPaymasterData(paymasterAndData []byte) (*OpenZeppelinPaymasterData, error) {
	if len(paymasterAndData) < 40 {
		return nil, fmt.Errorf("paymasterAndData too short for OpenZeppelin format: expected at least 40 bytes, got %d", len(paymasterAndData))
	}

	paymasterAddr := common.BytesToAddress(paymasterAndData[:20])
	tokenAddr := common.BytesToAddress(paymasterAndData[20:40])
	validationData := paymasterAndData[40:]

	return &OpenZeppelinPaymasterData{
		PaymasterAddress: paymasterAddr,
		TokenAddress:     tokenAddr,
		ValidationData:   validationData,
	}, nil
}

// ValidateOpenZeppelinPaymaster performs validation specific to OpenZeppelin PaymasterERC20
// This validates the format and structure, but actual token balance/price validation
// is done on-chain by the paymaster contract
func ValidateOpenZeppelinPaymaster(
	userOp *models.UserOperation,
	paymasterData *OpenZeppelinPaymasterData,
	logger zerolog.Logger,
) error {
	// OpenZeppelin PaymasterERC20 doesn't use signatures
	// Validation is based on:
	// 1. Token address (must be valid ERC-20)
	// 2. Token price/exchange rate (in validationData)
	// 3. User's token balance (checked on-chain)

	// Basic format validation
	if paymasterData.PaymasterAddress == (common.Address{}) {
		return fmt.Errorf("invalid paymaster address")
	}

	if paymasterData.TokenAddress == (common.Address{}) {
		return fmt.Errorf("invalid token address")
	}

	// Log validation data for debugging
	logger.Debug().
		Str("paymaster", paymasterData.PaymasterAddress.Hex()).
		Str("token", paymasterData.TokenAddress.Hex()).
		Int("validationDataLen", len(paymasterData.ValidationData)).
		Msg("OpenZeppelin PaymasterERC20 format validated")

	// Note: Actual token balance and price validation happens on-chain
	// in the paymaster's validatePaymasterUserOp function
	// We rely on simulateValidation to catch insufficient balances or invalid prices

	return nil
}

// EstimateOpenZeppelinPaymasterCost estimates the cost for OpenZeppelin PaymasterERC20
// This is a rough estimate - actual cost depends on token price and exchange rate
func EstimateOpenZeppelinPaymasterCost(
	userOp *models.UserOperation,
	paymasterData *OpenZeppelinPaymasterData,
) *big.Int {
	// Estimate gas cost in native currency
	gasCost := new(big.Int).Mul(
		userOp.MaxFeePerGas,
		new(big.Int).Add(
			userOp.CallGasLimit,
			new(big.Int).Add(
				userOp.VerificationGasLimit,
				userOp.PreVerificationGas,
			),
		),
	)

	// Note: Token price conversion would require:
	// 1. Token price from validationData or oracle
	// 2. Exchange rate calculation
	// This is simplified - actual conversion happens on-chain

	return gasCost
}

