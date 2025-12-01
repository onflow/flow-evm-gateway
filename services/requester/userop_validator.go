package requester

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
)

// UserOpValidator validates UserOperations before they are added to the pool
type UserOpValidator struct {
	client    *CrossSporkClient
	config    config.Config
	requester Requester
	blocks    storage.BlockIndexer
	logger    zerolog.Logger
}

func NewUserOpValidator(
	client *CrossSporkClient,
	config config.Config,
	requester Requester,
	blocks storage.BlockIndexer,
	logger zerolog.Logger,
) *UserOpValidator {
	logger = logger.With().Str("component", "userop-validator").Logger()

	// Log EntryPointSimulations configuration at startup
	// EntryPointSimulationsAddress should be required (validated at startup), but log for visibility
	if config.EntryPointSimulationsAddress != (common.Address{}) {
		logger.Info().
			Str("entryPointAddress", config.EntryPointAddress.Hex()).
			Str("entryPointSimulationsAddress", config.EntryPointSimulationsAddress.Hex()).
			Msg("EntryPointSimulations configured - will use for simulateValidation calls")
		// Note: Function existence verification happens on first UserOp validation
	} else {
		// This should not happen if validation is working, but log as error if it does
		logger.Error().
			Str("entryPointAddress", config.EntryPointAddress.Hex()).
			Msg("EntryPointSimulations not configured - this should have been caught at startup. Gateway will fail simulation calls.")
	}

	return &UserOpValidator{
		client:    client,
		config:    config,
		requester: requester,
		blocks:    blocks,
		logger:    logger,
	}
}

// VerifyEntryPointVersion verifies that the EntryPoint at the given address is v0.9.0
// by checking if senderCreator() function exists and is callable
func (v *UserOpValidator) VerifyEntryPointVersion(ctx context.Context, entryPoint common.Address) error {
	// Try to call senderCreator() - it should exist in v0.9.0
	calldata, err := EncodeSenderCreator()
	if err != nil {
		return fmt.Errorf("failed to encode senderCreator: %w", err)
	}

	height, err := v.blocks.LatestEVMHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest indexed height: %w", err)
	}

	txArgs := ethTypes.TransactionArgs{
		To:   &entryPoint,
		Data: (*hexutil.Bytes)(&calldata),
	}

	result, err := v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
	if err != nil {
		// If senderCreator() call fails, it might mean:
		// 1. EntryPoint is not v0.9.0 (older version without public getter)
		// 2. Wrong EntryPoint address
		// 3. ABI mismatch
		v.logger.Warn().
			Err(err).
			Str("entryPoint", entryPoint.Hex()).
			Msg("senderCreator() call failed - EntryPoint might not be v0.9.0 or ABI mismatch")
		return fmt.Errorf("EntryPoint version verification failed: senderCreator() call failed: %w", err)
	}

	// Decode result (should be 20-byte address)
	if len(result) >= 20 {
		senderCreatorAddr := common.BytesToAddress(result[len(result)-20:])
		v.logger.Info().
			Str("entryPoint", entryPoint.Hex()).
			Str("senderCreator", senderCreatorAddr.Hex()).
			Msg("EntryPoint version verified - senderCreator() exists (likely v0.9.0)")
		return nil
	}

	return fmt.Errorf("EntryPoint version verification failed: senderCreator() returned invalid data (length: %d)", len(result))
}

// Validate validates a UserOperation by calling EntryPoint.simulateValidation
func (v *UserOpValidator) Validate(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
) error {
	// Verify EntryPoint version on first validation (with caching in production)
	// For now, we'll verify but not fail if it doesn't work (to avoid breaking existing flows)
	if err := v.VerifyEntryPointVersion(ctx, entryPoint); err != nil {
		v.logger.Warn().
			Err(err).
			Msg("EntryPoint version verification failed - continuing with validation anyway")
		// Don't return error - just log warning
	}
	// Basic validation
	if err := v.validateBasic(userOp); err != nil {
		return err
	}

	// Verify signature
	// For account creation (initCode present), skip off-chain signature validation
	// because the sender doesn't exist yet. EntryPoint.simulateValidation will
	// correctly validate the signature against the owner address from initCode.
	chainID := v.config.EVMNetworkID
	isAccountCreation := len(userOp.InitCode) > 0

	if isAccountCreation {
		// For account creation, EntryPoint validates signature against owner from initCode
		v.logger.Debug().
			Str("sender", userOp.Sender.Hex()).
			Int("initCodeLen", len(userOp.InitCode)).
			Msg("skipping off-chain signature validation for account creation - EntryPoint will validate against owner")
	} else {
		// For existing accounts, validate signature against sender
		hash, err := userOp.Hash(entryPoint, chainID)
		if err != nil {
			return fmt.Errorf("failed to compute userOp hash: %w", err)
		}

		valid, err := userOp.VerifySignature(entryPoint, chainID)
		if err != nil {
			v.logger.Error().
				Err(err).
				Str("userOpHash", hash.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Uint("v", uint(userOp.Signature[64])).
				Msg("signature verification failed")
			return fmt.Errorf("signature verification failed: %w", err)
		}
		if !valid {
			// Recover address for logging
			sigHash := crypto.Keccak256Hash(
				[]byte("\x19\x01"),
				chainID.Bytes(),
				hash.Bytes(),
			)
			sigV := uint(userOp.Signature[64])
			pubKey, err := crypto.SigToPub(sigHash.Bytes(), append(userOp.Signature[:64], byte(sigV)))
			var recoveredAddr common.Address
			if err == nil {
				recoveredAddr = crypto.PubkeyToAddress(*pubKey)
			}

			v.logger.Error().
				Str("userOpHash", hash.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Str("recoveredAddr", recoveredAddr.Hex()).
				Uint("v", sigV).
				Msg("invalid user operation signature - recovered address does not match sender")
			return fmt.Errorf("invalid user operation signature: recovered address %s does not match sender %s", recoveredAddr.Hex(), userOp.Sender.Hex())
		}

		v.logger.Debug().
			Str("userOpHash", hash.Hex()).
			Str("sender", userOp.Sender.Hex()).
			Msg("off-chain signature validation passed for existing account")
	}

	// Simulate validation via EntryPoint
	if err := v.simulateValidation(ctx, userOp, entryPoint); err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	// Validate paymaster if present
	if len(userOp.PaymasterAndData) > 0 {
		if err := v.validatePaymaster(ctx, userOp, entryPoint); err != nil {
			return fmt.Errorf("paymaster validation failed: %w", err)
		}
	}

	return nil
}

// validateBasic performs basic validation checks
func (v *UserOpValidator) validateBasic(userOp *models.UserOperation) error {
	// Check required fields
	if userOp.Sender == (common.Address{}) {
		return fmt.Errorf("sender address is required")
	}
	if userOp.Nonce == nil {
		return fmt.Errorf("nonce is required")
	}
	if userOp.CallData == nil {
		return fmt.Errorf("callData is required")
	}
	if userOp.CallGasLimit == nil || userOp.CallGasLimit.Sign() <= 0 {
		return fmt.Errorf("callGasLimit must be positive")
	}
	if userOp.VerificationGasLimit == nil || userOp.VerificationGasLimit.Sign() <= 0 {
		return fmt.Errorf("verificationGasLimit must be positive")
	}
	if userOp.PreVerificationGas == nil || userOp.PreVerificationGas.Sign() <= 0 {
		return fmt.Errorf("preVerificationGas must be positive")
	}
	if userOp.MaxFeePerGas == nil || userOp.MaxFeePerGas.Sign() <= 0 {
		return fmt.Errorf("maxFeePerGas must be positive")
	}
	if userOp.MaxPriorityFeePerGas == nil || userOp.MaxPriorityFeePerGas.Sign() <= 0 {
		return fmt.Errorf("maxPriorityFeePerGas must be positive")
	}
	if len(userOp.Signature) == 0 {
		return fmt.Errorf("signature is required")
	}

	// Check gas limits are reasonable
	maxGas := big.NewInt(10_000_000) // 10M gas limit
	if userOp.CallGasLimit.Cmp(maxGas) > 0 {
		return fmt.Errorf("callGasLimit too high: %s", userOp.CallGasLimit.String())
	}
	if userOp.VerificationGasLimit.Cmp(maxGas) > 0 {
		return fmt.Errorf("verificationGasLimit too high: %s", userOp.VerificationGasLimit.String())
	}

	return nil
}

// simulateValidation calls EntryPoint.simulateValidation via eth_call
func (v *UserOpValidator) simulateValidation(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
) error {
	// Encode simulateValidation calldata
	calldata, err := EncodeSimulateValidation(userOp)
	if err != nil {
		return fmt.Errorf("failed to encode simulateValidation: %w", err)
	}

	// Get latest indexed block height (not network's latest, which may not be indexed yet)
	height, err := v.blocks.LatestEVMHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest indexed height: %w", err)
	}

	// Check if account already exists (for account creation UserOps)
	// If initCode is present but account already exists, EntryPoint will reject it
	if len(userOp.InitCode) > 0 {
		accountCode, err := v.requester.GetCode(userOp.Sender, height)
		if err == nil && len(accountCode) > 0 {
			// Account already exists - this will cause EntryPoint to reject the UserOp
			v.logger.Warn().
				Str("sender", userOp.Sender.Hex()).
				Int("codeLength", len(accountCode)).
				Msg("account already exists - EntryPoint will reject account creation UserOp")
			// Continue to simulateValidation - it will return the proper error
		} else if err == nil {
			// Account doesn't exist yet - this is expected for account creation
			v.logger.Info().
				Str("sender", userOp.Sender.Hex()).
				Int("codeLength", 0).
				Msg("account does not exist yet - proceeding with account creation")
		} else {
			// Error checking account code - log but continue
			v.logger.Warn().
				Err(err).
				Str("sender", userOp.Sender.Hex()).
				Msg("failed to check if account exists - continuing with validation")
		}
	}

	// Calculate UserOp hash for logging (EntryPoint v0.9.0 format)
	userOpHash, err := userOp.Hash(entryPoint, v.config.EVMNetworkID)
	if err != nil {
		v.logger.Warn().Err(err).Msg("failed to calculate UserOp hash for logging")
		userOpHash = common.Hash{} // Use zero hash if calculation fails
	}

	// Extract owner from initCode if present (for account creation)
	var ownerAddr common.Address
	var ownerExtracted bool
	if len(userOp.InitCode) > 0 {
		// Log initCode details for debugging
		if len(userOp.InitCode) >= 24 {
			factoryAddr := common.BytesToAddress(userOp.InitCode[0:20])
			selector := hexutil.Encode(userOp.InitCode[20:24])
			v.logger.Info().
				Str("factoryAddress", factoryAddr.Hex()).
				Str("functionSelector", selector).
				Int("initCodeLen", len(userOp.InitCode)).
				Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
				Msg("decoded initCode details")
		}

		owner, err := extractOwnerFromInitCode(userOp.InitCode)
		if err == nil {
			ownerAddr = owner
			ownerExtracted = true
		} else {
			v.logger.Warn().Err(err).Msg("failed to extract owner from initCode")
		}
	}

	// Recover signer from signature for logging
	var recoveredSigner common.Address
	var signatureRecovered bool
	if len(userOp.Signature) >= 65 {
		// IMPORTANT: The frontend signs the UserOp hash directly (not EIP-191 format)
		// We must use the UserOp hash for recovery, not a separate sigHash
		// The signature is over: userOpHash (not keccak256("\x19\x01" || chainId || userOpHash))
		hashForRecovery := userOpHash

		// Extract r, s, v from signature
		// Signature format: r (32 bytes) || s (32 bytes) || v (1 byte) = 65 bytes total
		r := userOp.Signature[0:32]
		s := userOp.Signature[32:64]
		sigV := userOp.Signature[64]

		// Convert EIP-155 v value (27/28) to recovery ID (0/1) for Ecrecover
		// Ecrecover expects recovery ID: v=27 -> recoveryID=0, v=28 -> recoveryID=1
		// For ERC-4337 (v=0/1), use directly as recovery ID
		recoveryID := sigV
		if sigV == 27 {
			recoveryID = 0
		} else if sigV == 28 {
			recoveryID = 1
		}
		signature := append(userOp.Signature[:64], recoveryID)

		// Log detailed signature parsing and recovery attempt
		v.logger.Info().
			Str("hashForRecovery", hashForRecovery.Hex()).
			Str("userOpHash", userOpHash.Hex()).
			Str("chainID", v.config.EVMNetworkID.String()).
			Int("signatureLength", len(userOp.Signature)).
			Str("r", hexutil.Encode(r)).
			Str("s", hexutil.Encode(s)).
			Uint8("signatureV", sigV).
			Uint8("recoveryID", recoveryID).
			Msg("attempting signature recovery using UserOp hash directly")

		// Handle both ERC-4337 format (v=0/1) and traditional Ethereum format (v=27/28)
		// Try with the converted recovery ID first
		// Use UserOp hash directly for recovery (frontend signs UserOp hash, not EIP-191 hash)
		var recoveryErr error
		pubKeyBytes, err := crypto.Ecrecover(hashForRecovery.Bytes(), signature)
		if err != nil {
			recoveryErr = err
		}

		if err == nil && len(pubKeyBytes) == 65 {
			// Extract address from public key: keccak256(pubKey[1:])[12:]
			// Ecrecover returns uncompressed public key (0x04 || x || y)
			pubKeyHash := crypto.Keccak256(pubKeyBytes[1:])
			recoveredSigner = common.BytesToAddress(pubKeyHash[12:])
			signatureRecovered = true
			v.logger.Info().
				Str("recoveredSigner", recoveredSigner.Hex()).
				Uint8("signatureV", sigV).
				Msg("signature recovery succeeded")
		} else {
			// If recovery failed, try alternative recovery IDs
			// For ERC-4337: try recoveryID=1 if recoveryID=0 failed, or vice versa
			// For traditional format: try recoveryID=1 if recoveryID=0 failed (v=27 -> recoveryID=0, v=28 -> recoveryID=1)
			var triedAlt bool
			if recoveryID == 0 {
				signatureAlt := append(userOp.Signature[:64], byte(1))
				pubKeyBytes, err := crypto.Ecrecover(hashForRecovery.Bytes(), signatureAlt)
				if err == nil && len(pubKeyBytes) == 65 {
					pubKeyHash := crypto.Keccak256(pubKeyBytes[1:])
					recoveredSigner = common.BytesToAddress(pubKeyHash[12:])
					signatureRecovered = true
					triedAlt = true
					v.logger.Info().
						Str("recoveredSigner", recoveredSigner.Hex()).
						Uint8("originalV", sigV).
						Uint8("originalRecoveryID", recoveryID).
						Uint8("usedRecoveryID", 1).
						Msg("signature recovery succeeded with alternative recovery ID")
				}
			} else if recoveryID == 1 {
				signatureAlt := append(userOp.Signature[:64], byte(0))
				pubKeyBytes, err := crypto.Ecrecover(hashForRecovery.Bytes(), signatureAlt)
				if err == nil && len(pubKeyBytes) == 65 {
					pubKeyHash := crypto.Keccak256(pubKeyBytes[1:])
					recoveredSigner = common.BytesToAddress(pubKeyHash[12:])
					signatureRecovered = true
					triedAlt = true
					v.logger.Info().
						Str("recoveredSigner", recoveredSigner.Hex()).
						Uint8("originalV", sigV).
						Uint8("originalRecoveryID", recoveryID).
						Uint8("usedRecoveryID", 0).
						Msg("signature recovery succeeded with alternative recovery ID")
				}
			}

			if !signatureRecovered {
				// Log detailed error information
				v.logger.Warn().
					Uint8("signatureV", sigV).
					Int("signatureLength", len(userOp.Signature)).
					Str("r", hexutil.Encode(r)).
					Str("s", hexutil.Encode(s)).
					Str("hashForRecovery", hashForRecovery.Hex()).
					Str("userOpHash", userOpHash.Hex()).
					Bool("triedAlternative", triedAlt).
					Err(recoveryErr).
					Msg("failed to recover signer from signature - ecrecover returned error or invalid public key")
			}
		}
	} else {
		v.logger.Warn().
			Int("signatureLength", len(userOp.Signature)).
			Msg("signature too short for recovery (expected 65 bytes)")
	}

	// Log EntryPoint address and UserOp details for debugging
	// Using Info level so this always shows (Debug might be filtered)
	logFields := v.logger.Info().
		Str("entryPoint", entryPoint.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("userOpHash", userOpHash.Hex()).
		Str("nonce", userOp.Nonce.String()).
		Int("initCodeLen", len(userOp.InitCode)).
		Int("callDataLen", len(userOp.CallData)).
		Str("maxFeePerGas", userOp.MaxFeePerGas.String()).
		Str("maxPriorityFeePerGas", userOp.MaxPriorityFeePerGas.String()).
		Str("callGasLimit", userOp.CallGasLimit.String()).
		Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
		Str("preVerificationGas", userOp.PreVerificationGas.String()).
		Int("signatureLen", len(userOp.Signature)).
		Uint64("height", height).
		Int("calldataLen", len(calldata)).
		Str("chainID", v.config.EVMNetworkID.String())

	// Add owner and signature recovery info if available
	if ownerExtracted {
		logFields = logFields.
			Str("ownerFromInitCode", ownerAddr.Hex()).
			Bool("ownerExtracted", true)
	}
	if signatureRecovered {
		logFields = logFields.
			Str("recoveredSigner", recoveredSigner.Hex()).
			Bool("signatureRecovered", true)
		if ownerExtracted {
			logFields = logFields.Bool("signerMatchesOwner", recoveredSigner == ownerAddr)
		}
	}

	// Add signature v value and full signature hex for debugging
	if len(userOp.Signature) >= 65 {
		logFields = logFields.
			Uint8("signatureV", userOp.Signature[64]).
			Str("signatureHex", hexutil.Encode(userOp.Signature)).
			Str("signatureR", hexutil.Encode(userOp.Signature[0:32])).
			Str("signatureS", hexutil.Encode(userOp.Signature[32:64]))
	}

	// Determine which contract to call for simulation
	// EntryPointSimulationsAddress is REQUIRED for v0.7+ EntryPoints
	// This should have been validated at startup, but check again for safety
	if v.config.EntryPointSimulationsAddress == (common.Address{}) {
		return fmt.Errorf("EntryPointSimulations address not configured - required for EntryPoint v0.7+. Configure --entry-point-simulations-address")
	}

	simulationAddress := v.config.EntryPointSimulationsAddress

	// Log that we're using EntryPointSimulations
	// Note: Even if RPC can't see the contract (sync/indexing issue), we proceed with the call
	// The contract is verified on Flowscan at this address, so we trust the configuration
	v.logger.Info().
		Str("entryPoint", entryPoint.Hex()).
		Str("entryPointSimulationsAddress", v.config.EntryPointSimulationsAddress.Hex()).
		Str("simulationAddress", simulationAddress.Hex()).
		Msg("using EntryPointSimulations contract for simulateValidation (v0.7+) - proceeding even if RPC can't see contract (may be sync/indexing issue)")

	// Log the exact calldata being sent to EntryPoint
	logFields = logFields.Str("calldataHex", hexutil.Encode(calldata)).Int("calldataLen", len(calldata))
	logFields = logFields.Str("simulationAddress", simulationAddress.Hex())
	logFields.Msg("calling simulateValidation with full UserOp details")

	// Create transaction args for eth_call
	txArgs := ethTypes.TransactionArgs{
		To:   &simulationAddress,
		Data: (*hexutil.Bytes)(&calldata),
	}

	// Call simulateValidation (on EntryPointSimulations for v0.7+, or EntryPoint for v0.6)
	// simulateValidation always reverts (either with ValidationResult for success or FailedOp for failure)
	_, err = v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
	if err != nil {
		// Log detailed error for debugging
		v.logger.Error().
			Err(err).
			Str("entryPoint", entryPoint.Hex()).
			Str("simulationAddress", simulationAddress.Hex()).
			Str("sender", userOp.Sender.Hex()).
			Uint64("height", height).
			Bool("usingSimulationsContract", v.config.EntryPointSimulationsAddress != (common.Address{})).
			Msg("simulateValidation call failed")

		// Check if it's a revert error (expected - simulateValidation always reverts with result)
		if revertErr, ok := err.(*errs.RevertError); ok {
			// Decode revert reason hex string to bytes for better logging
			var revertData []byte
			if revertErr.Reason != "" && revertErr.Reason != "0x" {
				var decodeErr error
				revertData, decodeErr = hexutil.Decode(revertErr.Reason)
				if decodeErr != nil {
					// If decode fails, treat as raw string
					revertData = []byte(revertErr.Reason)
				}
			}

			// Decode revert data to determine if it's success (ValidationResult) or failure (FailedOp)
			decodedResult := v.decodeRevertData(revertData, revertErr.Reason)

			// Build detailed revert log with all available context
			revertLog := v.logger.Info(). // Use Info level - reverts are expected
							Str("revertReasonHex", revertErr.Reason).
							Int("revertDataLen", len(revertData)).
							Str("entryPoint", entryPoint.Hex()).
							Str("sender", userOp.Sender.Hex()).
							Str("userOpHash", userOpHash.Hex()).
							Str("nonce", userOp.Nonce.String()).
							Int("initCodeLen", len(userOp.InitCode)).
							Uint64("height", height)

			// Add owner and signature recovery info if available
			if ownerExtracted {
				revertLog = revertLog.Str("ownerFromInitCode", ownerAddr.Hex())
			}
			if signatureRecovered {
				revertLog = revertLog.Str("recoveredSigner", recoveredSigner.Hex())
				if ownerExtracted {
					revertLog = revertLog.Bool("signerMatchesOwner", recoveredSigner == ownerAddr)
				}
			}

			// Add decoded result info
			if decodedResult.Decoded != "" {
				revertLog = revertLog.Str("decodedResult", decodedResult.Decoded)
			}
			if decodedResult.IsValidationResult {
				revertLog = revertLog.Bool("isValidationResult", true)
			}
			if decodedResult.IsFailedOp {
				revertLog = revertLog.Bool("isFailedOp", true)
			}
			if decodedResult.AAErrorCode != "" {
				revertLog = revertLog.Str("aaErrorCode", decodedResult.AAErrorCode)
			}

			revertLog.Msg("EntryPoint.simulateValidation reverted (expected behavior)")

			// If it's a ValidationResult, this is SUCCESS - validation passed
			if decodedResult.IsValidationResult {
				v.logger.Info().
					Str("decodedResult", decodedResult.Decoded).
					Msg("simulateValidation succeeded - ValidationResult indicates validation passed")
				// Validation passed - return nil (no error)
				return nil
			}

			// If it's a FailedOp or other error, this is a validation failure
			if decodedResult.IsFailedOp || decodedResult.AAErrorCode != "" {
				var errorMsg string
				if decodedResult.AAErrorCode != "" {
					errorMsg = fmt.Sprintf("validation failed: %s (AA error: %s)", decodedResult.Decoded, decodedResult.AAErrorCode)
				} else {
					errorMsg = fmt.Sprintf("validation failed: %s", decodedResult.Decoded)
				}
				v.logger.Error().
					Str("decodedResult", decodedResult.Decoded).
					Str("aaErrorCode", decodedResult.AAErrorCode).
					Str("revertReasonHex", revertErr.Reason).
					Msg("simulateValidation failed - validation error detected")
				return fmt.Errorf("%s", errorMsg)
			}

			// If we couldn't decode, check if it's an empty revert
			// Empty revert (0x, length 0) can happen for various reasons:
			// 1. Function doesn't exist (falls through to fallback)
			// 2. Function exists but reverts without data
			// 3. Validation failed but error wasn't properly encoded
			// 4. RPC sync/indexing issue - contract exists but RPC can't see it properly
			if len(revertData) == 0 {
				// Extract selector for logging
				selectorHex := hexutil.Encode(calldata[:4])
				v.logger.Error().
					Str("revertReasonHex", revertErr.Reason).
					Int("revertDataLen", len(revertData)).
					Str("entryPoint", entryPoint.Hex()).
					Str("simulationAddress", simulationAddress.Hex()).
					Str("functionSelector", selectorHex).
					Str("sender", userOp.Sender.Hex()).
					Msg("simulateValidation reverted with empty data - cannot determine validation result. Note: Contract is verified on Flowscan at this address. If RPC can't see the contract, this may be an RPC sync/indexing issue. Otherwise, this may indicate the function doesn't exist, validation failed without proper error encoding, or a contract implementation issue.")
				return fmt.Errorf("validation reverted with empty data - simulateValidation call to %s (verified on Flowscan) returned no error data. This may be an RPC sync/indexing issue if the RPC can't see the contract. Otherwise, verify the contract has simulateValidation function and check contract implementation", simulationAddress.Hex())
			}

			// Unknown format - log but don't fail (might be ValidationResult)
			v.logger.Warn().
				Str("decodedResult", decodedResult.Decoded).
				Str("revertReasonHex", revertErr.Reason).
				Int("revertDataLen", len(revertData)).
				Msg("simulateValidation reverted with unknown format - cannot determine if validation passed. Treating as failure for safety.")
			return fmt.Errorf("validation reverted with unknown format: %s", decodedResult.Decoded)
		}
		return fmt.Errorf("simulation call failed: %w", err)
	}

	// If we get here, simulateValidation didn't revert
	// This is unexpected - simulateValidation should always revert with ValidationResult or FailedOp
	// Log warning but treat as success (might be a different EntryPoint version)
	v.logger.Warn().
		Str("entryPoint", entryPoint.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Msg("simulateValidation returned without revert - unexpected behavior, treating as success")

	return nil
}

// validatePaymaster validates paymaster deposit and signature
func (v *UserOpValidator) validatePaymaster(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
) error {
	// Extract paymaster address from paymasterAndData
	// First 20 bytes are the paymaster address
	if len(userOp.PaymasterAndData) < 20 {
		return fmt.Errorf("paymasterAndData too short")
	}

	paymasterAddr := common.BytesToAddress(userOp.PaymasterAndData[:20])

	// Check paymaster deposit in EntryPoint
	deposit, err := v.getPaymasterDeposit(ctx, paymasterAddr, entryPoint)
	if err != nil {
		return fmt.Errorf("failed to check paymaster deposit: %w", err)
	}

	// Estimate required deposit (rough estimate: maxFeePerGas * (callGasLimit + verificationGasLimit + preVerificationGas))
	requiredDeposit := new(big.Int).Mul(
		userOp.MaxFeePerGas,
		new(big.Int).Add(
			userOp.CallGasLimit,
			new(big.Int).Add(
				userOp.VerificationGasLimit,
				userOp.PreVerificationGas,
			),
		),
	)

	if deposit.Cmp(requiredDeposit) < 0 {
		return fmt.Errorf("insufficient paymaster deposit: have %s, need at least %s", deposit.String(), requiredDeposit.String())
	}

	// Validate paymaster format based on implementation
	// We support OpenZeppelin PaymasterERC20 as the standard implementation
	if len(userOp.PaymasterAndData) >= 40 {
		// Try to parse as OpenZeppelin PaymasterERC20 format
		ozData, err := ParseOpenZeppelinPaymasterData(userOp.PaymasterAndData)
		if err == nil {
			// Validate OpenZeppelin format
			if err := ValidateOpenZeppelinPaymaster(userOp, ozData, v.logger); err != nil {
				return fmt.Errorf("OpenZeppelin paymaster validation failed: %w", err)
			}
			// OpenZeppelin PaymasterERC20 doesn't use signatures
			// Token balance and price validation happens on-chain
			v.logger.Debug().
				Str("paymaster", paymasterAddr.Hex()).
				Str("token", ozData.TokenAddress.Hex()).
				Msg("OpenZeppelin PaymasterERC20 validated")
		} else {
			// Not OpenZeppelin format - could be VerifyingPaymaster or custom
			// For other paymaster types, we rely on simulateValidation
			v.logger.Debug().
				Str("paymaster", paymasterAddr.Hex()).
				Msg("non-OpenZeppelin paymaster format, relying on simulateValidation")
		}
	}

	// Note: For non-OpenZeppelin paymasters (e.g., VerifyingPaymaster with signatures),
	// we rely on simulateValidation to catch invalid signatures, as:
	// 1. Signature formats vary by paymaster implementation
	// 2. Full validation requires paymaster-specific logic
	// 3. simulateValidation will revert if signature is invalid
	//
	// See docs/PAYMASTER_VALIDATION.md and docs/OPENZEPPELIN_PAYMASTER.md for details

	v.logger.Debug().
		Str("paymaster", paymasterAddr.Hex()).
		Str("deposit", deposit.String()).
		Msg("paymaster validation passed")

	return nil
}

// getPaymasterDeposit retrieves the paymaster's deposit from EntryPoint
func (v *UserOpValidator) getPaymasterDeposit(
	ctx context.Context,
	paymasterAddr common.Address,
	entryPoint common.Address,
) (*big.Int, error) {
	// Encode getDeposit calldata
	calldata, err := EncodeGetDeposit(paymasterAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to encode getDeposit: %w", err)
	}

	// Get latest indexed block height (not network's latest, which may not be indexed yet)
	height, err := v.blocks.LatestEVMHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest indexed height: %w", err)
	}

	// Create transaction args for eth_call
	txArgs := ethTypes.TransactionArgs{
		To:   &entryPoint,
		Data: (*hexutil.Bytes)(&calldata),
	}

	// Call EntryPoint.getDeposit
	result, err := v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call getDeposit: %w", err)
	}

	// Decode result (uint256)
	if len(result) < 32 {
		return nil, fmt.Errorf("invalid getDeposit result: expected 32 bytes, got %d", len(result))
	}

	deposit := new(big.Int).SetBytes(result)
	return deposit, nil
}

// EstimateGas estimates gas for a UserOperation
func (v *UserOpValidator) EstimateGas(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
) (*UserOpGasEstimate, error) {
	// Encode simulateValidation calldata
	calldata, err := EncodeSimulateValidation(userOp)
	if err != nil {
		return nil, fmt.Errorf("failed to encode simulateValidation: %w", err)
	}

	// Get latest indexed block height (not network's latest, which may not be indexed yet)
	height, err := v.blocks.LatestEVMHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest indexed height: %w", err)
	}

	// Create transaction args for eth_estimateGas
	txArgs := ethTypes.TransactionArgs{
		To:   &entryPoint,
		Data: (*hexutil.Bytes)(&calldata),
	}

	// Estimate gas
	gasLimit, err := v.requester.EstimateGas(txArgs, v.config.Coinbase, height, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas: %w", err)
	}

	// Parse gas estimates from simulation result
	// simulateValidation returns gas estimates in revert data on success
	// For now, use the estimated gas and split it
	verificationGas := big.NewInt(int64(gasLimit) * 2 / 3) // ~66% for verification
	preVerificationGas := big.NewInt(21000)                // Base overhead
	callGasLimit := userOp.CallGasLimit
	if callGasLimit == nil {
		callGasLimit = big.NewInt(20000) // Default
	}

	return &UserOpGasEstimate{
		PreVerificationGas: hexutil.Big(*preVerificationGas),
		VerificationGas:    hexutil.Big(*verificationGas),
		CallGasLimit:       hexutil.Big(*callGasLimit),
	}, nil
}

// UserOpGasEstimate represents gas estimates for a UserOperation
type UserOpGasEstimate struct {
	PreVerificationGas hexutil.Big `json:"preVerificationGas"`
	VerificationGas    hexutil.Big `json:"verificationGas"`
	CallGasLimit       hexutil.Big `json:"callGasLimit"`
}

// extractOwnerFromInitCode extracts the owner address from SimpleAccountFactory.createAccount(owner, salt) initCode
// Format: factoryAddress (20 bytes) + functionSelector (4 bytes) + ABI-encoded params
// ABI encoding for createAccount(address owner, uint256 salt):
//   - First parameter (address owner): 32 bytes (address padded to 32 bytes, address is last 20 bytes)
//   - Second parameter (uint256 salt): 32 bytes
//
// Structure:
//
//	Bytes 0-19: Factory address
//	Bytes 20-23: Function selector (createAccount)
//	Bytes 24-55: Owner address (32 bytes, address is last 20 bytes = bytes 36-55)
//	Bytes 56-87: Salt (uint256)
func extractOwnerFromInitCode(initCode []byte) (common.Address, error) {
	// Minimum length: 20 (factory) + 4 (selector) + 32 (owner) + 32 (salt) = 88 bytes
	if len(initCode) < 88 {
		return common.Address{}, fmt.Errorf("initCode too short: %d bytes (expected at least 88)", len(initCode))
	}

	// Owner address is the first parameter, encoded as 32 bytes with address in last 20 bytes
	// Owner param starts at byte 24, address is at bytes 36-55 (last 20 bytes of the 32-byte word)
	ownerStart := 36
	if len(initCode) < ownerStart+20 {
		return common.Address{}, fmt.Errorf("initCode too short for owner extraction: %d bytes (need at least %d)", len(initCode), ownerStart+20)
	}

	// Extract owner address (last 20 bytes of the 32-byte word starting at byte 24)
	ownerBytes := initCode[ownerStart : ownerStart+20]
	return common.BytesToAddress(ownerBytes), nil
}

// RevertDecodeResult contains decoded revert information
type RevertDecodeResult struct {
	Decoded            string // Human-readable decoded message
	IsValidationResult bool   // True if this is a ValidationResult (success)
	IsFailedOp         bool   // True if this is a FailedOp error
	AAErrorCode        string // AAxx error code if detected (e.g., "AA13", "AA20")
}

// decodeRevertData attempts to decode revert data and determine if it's success or failure
func (v *UserOpValidator) decodeRevertData(revertData []byte, revertHex string) RevertDecodeResult {
	result := RevertDecodeResult{}

	if len(revertData) < 4 {
		result.Decoded = "Revert without reason (empty or selector only)"
		return result
	}

	errorSelector := hexutil.Encode(revertData[:4])

	// Strategy 1: Check for FailedOp errors (validation failures)
	failedOpSelector := crypto.Keccak256([]byte("FailedOp(uint256,string)"))[:4]
	if hexutil.Encode(revertData[:4]) == hexutil.Encode(failedOpSelector) {
		result.IsFailedOp = true
		decoded := v.decodeFailedOp(revertData)
		result.Decoded = decoded.Decoded
		result.AAErrorCode = decoded.AAErrorCode
		return result
	}

	// Strategy 2: Check for FailedOpWithRevert
	failedOpWithRevertSelector := crypto.Keccak256([]byte("FailedOpWithRevert(uint256,string,bytes)"))[:4]
	if hexutil.Encode(revertData[:4]) == hexutil.Encode(failedOpWithRevertSelector) {
		result.IsFailedOp = true
		decoded := v.decodeFailedOpWithRevert(revertData)
		result.Decoded = decoded.Decoded
		result.AAErrorCode = decoded.AAErrorCode
		return result
	}

	// Strategy 3: Check for ValidationResult struct
	// ValidationResult is not an error - it's the success case
	// Format varies by EntryPoint version, but typically contains gas estimates
	// For v0.9, ValidationResult might be encoded directly (no selector) or with a specific format
	// If it's not a FailedOp and has substantial data, it might be ValidationResult
	if len(revertData) >= 32 && errorSelector != "0x08c379a0" {
		// Check if it looks like ValidationResult (structured data, not an error)
		// ValidationResult typically has multiple uint256 fields (gas estimates)
		result.IsValidationResult = true
		result.Decoded = v.decodeValidationResult(revertData)
		return result
	}

	// Strategy 4: Standard Error(string)
	if errorSelector == "0x08c379a0" {
		decoded := v.decodeErrorString(revertData)
		result.Decoded = decoded
		// Check if it contains AA error code
		if aaCode := v.extractAAErrorCode(decoded); aaCode != "" {
			result.AAErrorCode = aaCode
			result.IsFailedOp = true
		}
		return result
	}

	// Strategy 5: Unknown format - log selector for investigation
	result.Decoded = fmt.Sprintf("Unknown error format (selector: %s, data length: %d bytes)", errorSelector, len(revertData))
	v.logger.Info().
		Str("errorSelector", errorSelector).
		Str("revertDataHex", revertHex).
		Int("revertDataLen", len(revertData)).
		Msg("EntryPoint revert with unknown selector - may be ValidationResult or custom error")
	return result
}

// decodeFailedOp decodes FailedOp(uint256,string) error
func (v *UserOpValidator) decodeFailedOp(revertData []byte) RevertDecodeResult {
	result := RevertDecodeResult{IsFailedOp: true}
	if len(revertData) < 100 {
		result.Decoded = "FailedOp (insufficient data)"
		return result
	}

	opIndex := new(big.Int).SetBytes(revertData[4:36])
	offset := new(big.Int).SetBytes(revertData[36:68])
	if offset.Cmp(big.NewInt(64)) == 0 && len(revertData) >= 100 {
		strLen := new(big.Int).SetBytes(revertData[68:100])
		if strLen.Cmp(big.NewInt(0)) > 0 {
			strLenInt := int(strLen.Int64())
			if len(revertData) >= 100+strLenInt {
				strBytes := revertData[100 : 100+strLenInt]
				// Remove null padding
				for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
					strBytes = strBytes[:len(strBytes)-1]
				}
				if len(strBytes) > 0 {
					reason := string(strBytes)
					result.Decoded = fmt.Sprintf("FailedOp(opIndex=%s, reason=%q)", opIndex.String(), reason)
					result.AAErrorCode = v.extractAAErrorCode(reason)
				}
			}
		}
	}
	return result
}

// decodeFailedOpWithRevert decodes FailedOpWithRevert(uint256,string,bytes) error
func (v *UserOpValidator) decodeFailedOpWithRevert(revertData []byte) RevertDecodeResult {
	result := RevertDecodeResult{IsFailedOp: true}
	if len(revertData) < 100 {
		result.Decoded = "FailedOpWithRevert (insufficient data)"
		return result
	}

	opIndex := new(big.Int).SetBytes(revertData[4:36])
	offset := new(big.Int).SetBytes(revertData[36:68])
	if offset.Cmp(big.NewInt(96)) == 0 && len(revertData) >= 132 {
		strLen := new(big.Int).SetBytes(revertData[100:132])
		if strLen.Cmp(big.NewInt(0)) > 0 {
			strLenInt := int(strLen.Int64())
			if len(revertData) >= 132+strLenInt {
				strBytes := revertData[132 : 132+strLenInt]
				// Remove null padding
				for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
					strBytes = strBytes[:len(strBytes)-1]
				}
				if len(strBytes) > 0 {
					reason := string(strBytes)
					result.Decoded = fmt.Sprintf("FailedOpWithRevert(opIndex=%s, reason=%q)", opIndex.String(), reason)
					result.AAErrorCode = v.extractAAErrorCode(reason)
				}
			}
		}
	}
	return result
}

// decodeValidationResult attempts to decode ValidationResult struct
// ValidationResult format varies, but typically contains gas estimates
func (v *UserOpValidator) decodeValidationResult(revertData []byte) string {
	// ValidationResult typically has multiple uint256 fields
	// Format: preOpGas (32) + paid (32) + validAfter (32) + validUntil (32) + paymasterContext offset/length
	if len(revertData) >= 128 {
		preOpGas := new(big.Int).SetBytes(revertData[0:32])
		paid := new(big.Int).SetBytes(revertData[32:64])
		validAfter := new(big.Int).SetBytes(revertData[64:96])
		validUntil := new(big.Int).SetBytes(revertData[96:128])
		return fmt.Sprintf("ValidationResult(preOpGas=%s, paid=%s, validAfter=%s, validUntil=%s)", preOpGas.String(), paid.String(), validAfter.String(), validUntil.String())
	}
	return fmt.Sprintf("ValidationResult (data length: %d bytes)", len(revertData))
}

// decodeErrorString decodes standard Error(string) revert
func (v *UserOpValidator) decodeErrorString(revertData []byte) string {
	if len(revertData) < 68 {
		return "Error(string) (insufficient data)"
	}
	offset := new(big.Int).SetBytes(revertData[4:36])
	if offset.Cmp(big.NewInt(32)) == 0 {
		strLen := new(big.Int).SetBytes(revertData[36:68])
		if strLen.Cmp(big.NewInt(0)) > 0 {
			strLenInt := int(strLen.Int64())
			if len(revertData) >= 68+strLenInt {
				strBytes := revertData[68 : 68+strLenInt]
				for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
					strBytes = strBytes[:len(strBytes)-1]
				}
				if len(strBytes) > 0 {
					return fmt.Sprintf("Error(string): %s", string(strBytes))
				}
			}
		}
	}
	return "Error(string) (could not decode)"
}

// extractAAErrorCode extracts AAxx error code from error message
func (v *UserOpValidator) extractAAErrorCode(message string) string {
	// Look for AA followed by digits (e.g., "AA13", "AA20", "AA23")
	// Common patterns: "AA13", "AA20", "AA23", "AA10", "AA21", "AA22"
	for i := 0; i < len(message)-3; i++ {
		if message[i] == 'A' && message[i+1] == 'A' {
			if message[i+2] >= '0' && message[i+2] <= '9' && message[i+3] >= '0' && message[i+3] <= '9' {
				return message[i : i+4]
			}
		}
	}
	return ""
}

// decodeRevertReason attempts to decode revert data using multiple strategies
// Returns decoded reason string if successful, empty string otherwise
// DEPRECATED: Use decodeRevertData instead for better error handling
func (v *UserOpValidator) decodeRevertReason(revertData []byte, revertHex string) string {
	// Strategy 1: Try to decode as standard Error(string) revert
	// Standard revert format: 0x08c379a0 (Error(string) selector) + offset + length + string
	if len(revertData) >= 4 {
		errorSelector := hexutil.Encode(revertData[:4])
		// Error(string) selector: 0x08c379a0
		if errorSelector == "0x08c379a0" && len(revertData) >= 68 {
			// Try to decode as Error(string)
			// Format: selector (4) + offset (32) + length (32) + string data
			// Offset should be 0x20 (32) for Error(string)
			offset := new(big.Int).SetBytes(revertData[4:36])
			if offset.Cmp(big.NewInt(32)) == 0 && len(revertData) >= 68 {
				// Get string length
				strLen := new(big.Int).SetBytes(revertData[36:68])
				if strLen.Cmp(big.NewInt(0)) > 0 {
					strLenInt := int(strLen.Int64())
					if len(revertData) >= 68+strLenInt {
						// Extract string (may be padded)
						strBytes := revertData[68 : 68+strLenInt]
						// Remove null padding
						for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
							strBytes = strBytes[:len(strBytes)-1]
						}
						if len(strBytes) > 0 {
							return fmt.Sprintf("Error(string): %s", string(strBytes))
						}
					}
				}
			}
		}

		// Strategy 2: Try to decode EntryPoint v0.9.0 custom errors
		// FailedOp(uint256 opIndex, string reason)
		// Selector: keccak256("FailedOp(uint256,string)")[:4] = 0x220266b6
		failedOpSelector := crypto.Keccak256([]byte("FailedOp(uint256,string)"))[:4]
		if len(revertData) >= 4 && hexutil.Encode(revertData[:4]) == hexutil.Encode(failedOpSelector) {
			// Format: selector (4) + opIndex (32) + string offset (32) + string length (32) + string data
			if len(revertData) >= 100 {
				opIndex := new(big.Int).SetBytes(revertData[4:36])
				offset := new(big.Int).SetBytes(revertData[36:68])
				if offset.Cmp(big.NewInt(64)) == 0 && len(revertData) >= 100 {
					strLen := new(big.Int).SetBytes(revertData[68:100])
					if strLen.Cmp(big.NewInt(0)) > 0 {
						strLenInt := int(strLen.Int64())
						if len(revertData) >= 100+strLenInt {
							strBytes := revertData[100 : 100+strLenInt]
							// Remove null padding
							for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
								strBytes = strBytes[:len(strBytes)-1]
							}
							if len(strBytes) > 0 {
								return fmt.Sprintf("FailedOp(opIndex=%s, reason=%q)", opIndex.String(), string(strBytes))
							}
						}
					}
				}
			}
		}

		// FailedOpWithRevert(uint256 opIndex, string reason, bytes revertData)
		// Selector: keccak256("FailedOpWithRevert(uint256,string,bytes)")[:4] = 0x220266b6 (same as FailedOp, but different params)
		// Actually, let's check for this separately - it has 3 params so the encoding is different
		failedOpWithRevertSelector := crypto.Keccak256([]byte("FailedOpWithRevert(uint256,string,bytes)"))[:4]
		if len(revertData) >= 4 && hexutil.Encode(revertData[:4]) == hexutil.Encode(failedOpWithRevertSelector) {
			// Format: selector (4) + opIndex (32) + string offset (32) + bytes offset (32) + string length (32) + string data + bytes length (32) + bytes data
			// This is more complex, so we'll just identify it for now
			if len(revertData) >= 100 {
				opIndex := new(big.Int).SetBytes(revertData[4:36])
				// Try to extract reason string (similar to FailedOp)
				offset := new(big.Int).SetBytes(revertData[36:68])
				if offset.Cmp(big.NewInt(96)) == 0 && len(revertData) >= 132 {
					strLen := new(big.Int).SetBytes(revertData[100:132])
					if strLen.Cmp(big.NewInt(0)) > 0 {
						strLenInt := int(strLen.Int64())
						if len(revertData) >= 132+strLenInt {
							strBytes := revertData[132 : 132+strLenInt]
							// Remove null padding
							for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
								strBytes = strBytes[:len(strBytes)-1]
							}
							if len(strBytes) > 0 {
								// Also try to get revert data length
								bytesOffset := 132 + ((strLenInt+31)/32)*32
								if len(revertData) >= bytesOffset+32 {
									bytesLen := new(big.Int).SetBytes(revertData[bytesOffset : bytesOffset+32])
									return fmt.Sprintf("FailedOpWithRevert(opIndex=%s, reason=%q, revertDataLen=%s)", opIndex.String(), string(strBytes), bytesLen.String())
								}
								return fmt.Sprintf("FailedOpWithRevert(opIndex=%s, reason=%q)", opIndex.String(), string(strBytes))
							}
						}
					}
				}
			}
		}

		// Strategy 3: Try to identify other EntryPoint v0.9.0 custom error selectors
		// Common EntryPoint errors:
		// - ValidationResult - selector varies (this is a return value, not an error)
		// Log any other custom error selectors for manual investigation
		customErrorSelector := hexutil.Encode(revertData[:4])
		if customErrorSelector != "0x08c379a0" &&
			hexutil.Encode(revertData[:4]) != hexutil.Encode(failedOpSelector) &&
			hexutil.Encode(revertData[:4]) != hexutil.Encode(failedOpWithRevertSelector) {
			// This might be a custom error - log the selector for manual investigation
			// Use Info level so it's always visible (Debug might be filtered)
			v.logger.Info().
				Str("errorSelector", customErrorSelector).
				Str("revertDataHex", revertHex).
				Int("revertDataLen", len(revertData)).
				Msg("EntryPoint revert with custom error selector (not Error(string) or FailedOp) - may be EntryPoint ValidationResult or other custom error")
			return fmt.Sprintf("Custom error (selector: %s, data length: %d bytes) - may be EntryPoint ValidationResult or other custom error", customErrorSelector, len(revertData))
		}
	}

	// Strategy 3: If revert data is very short, it might be a simple revert without reason
	if len(revertData) <= 4 {
		return "Revert without reason (empty or selector only)"
	}

	// Strategy 4: Try to decode as raw string (non-standard, but some contracts do this)
	// Remove null padding and try to interpret as UTF-8
	trimmed := revertData
	for len(trimmed) > 0 && trimmed[len(trimmed)-1] == 0 {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if len(trimmed) > 4 {
		// Skip selector if present
		start := 0
		if len(trimmed) >= 4 {
			// Check if first 4 bytes look like a selector
			start = 4
		}
		if len(trimmed) > start {
			// Try to decode as UTF-8 string
			if str := string(trimmed[start:]); len(str) > 0 {
				// Basic check: all bytes are printable ASCII
				allPrintable := true
				for _, b := range str {
					if b < 32 || b > 126 {
						allPrintable = false
						break
					}
				}
				if allPrintable && len(str) > 0 {
					return fmt.Sprintf("Raw string: %s", str)
				}
			}
		}
	}

	// Could not decode
	return ""
}
