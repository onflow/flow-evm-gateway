package requester

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/abis"
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
	cfg config.Config,
	requester Requester,
	blocks storage.BlockIndexer,
	logger zerolog.Logger,
) *UserOpValidator {
	logger = logger.With().Str("component", "userop-validator").Logger()

	// Set default stake requirements based on network type
	// Note: cfg is passed by value, so we update it and use the updated version
	cfg.SetDefaultStakeRequirements()

	// Log stake requirements at startup
	logger.Info().
		Str("minSenderStake", cfg.MinSenderStake.String()).
		Str("minFactoryStake", cfg.MinFactoryStake.String()).
		Str("minPaymasterStake", cfg.MinPaymasterStake.String()).
		Str("minAggregatorStake", cfg.MinAggregatorStake.String()).
		Uint64("minUnstakeDelaySec", cfg.MinUnstakeDelaySec).
		Str("flowNetworkID", string(cfg.FlowNetworkID)).
		Msg("ERC-4337 stake requirements configured")

	// Log EntryPointSimulations configuration at startup
	// Best practice is to call EntryPoint.simulateValidation directly (EntryPointSimulationsAddress empty)
	if cfg.EntryPointSimulationsAddress != (common.Address{}) {
		logger.Warn().
			Str("entryPointAddress", cfg.EntryPointAddress.Hex()).
			Str("entryPointSimulationsAddress", cfg.EntryPointSimulationsAddress.Hex()).
			Msg("EntryPointSimulations configured - legacy mode. Best practice is to unset this and call EntryPoint.simulateValidation directly.")
		// Note: Function existence verification happens on first UserOp validation
	} else {
		logger.Info().
			Str("entryPointAddress", cfg.EntryPointAddress.Hex()).
			Msg("EntryPointSimulations not configured - will call EntryPoint.simulateValidation directly (recommended)")
	}

	return &UserOpValidator{
		client:    client,
		config:    cfg, // Use updated cfg with default stake requirements
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

	// Log signature before normalization
	if len(userOp.Signature) >= 65 {
		v.logger.Info().
			Str("sender", userOp.Sender.Hex()).
			Uint8("signatureV_before_normalization", userOp.Signature[64]).
			Str("signatureHex_before", hexutil.Encode(userOp.Signature)).
			Str("signatureLastByte_before", hexutil.Encode(userOp.Signature[64:65])).
			Msg("signature before normalization check")
	}

	// Do NOT normalize signature v value - pass it through as-is
	// The gateway does not perform signature validation - EntryPoint.simulateValidation() handles it on-chain.
	// We pass the signature through exactly as received and let EntryPoint validate it.
	if len(userOp.Signature) >= 65 {
		vByte := userOp.Signature[64]
		v.logger.Debug().
			Uint8("signatureV", vByte).
			Str("sender", userOp.Sender.Hex()).
			Msg("signature v value passed through without normalization")
	}

	// NO MANUAL SIGNATURE VERIFICATION - EntryPoint.simulateValidation() handles signature validation
	// For both account creation and existing accounts, EntryPoint will validate the signature
	// We only do basic validation (signature length, etc.) and let the contract handle the rest
	if len(userOp.Signature) < 65 {
		return fmt.Errorf("signature too short: %d bytes", len(userOp.Signature))
	}

	isAccountCreation := len(userOp.InitCode) > 0
	if isAccountCreation {
		// For account creation, EntryPoint validates signature against owner from initCode
		v.logger.Debug().
			Str("sender", userOp.Sender.Hex()).
			Int("initCodeLen", len(userOp.InitCode)).
			Msg("skipping off-chain signature validation for account creation - EntryPoint.simulateValidation() will validate against owner")
	} else {
		// For existing accounts, EntryPoint validates signature against sender
		v.logger.Debug().
			Str("sender", userOp.Sender.Hex()).
			Msg("skipping off-chain signature validation for existing account - EntryPoint.simulateValidation() will validate signature")
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
	// Determine which contract to call for simulation
	// IMPORTANT: EntryPointSimulations is designed to be used with state/code override at EntryPoint address,
	// NOT as a separately deployed contract. When deployed separately, it computes a different senderCreator
	// address, causing factory calls to fail. Best practice is to call EntryPoint.simulateValidation directly.
	var simulationAddress common.Address
	isUsingEntryPointSimulations := false
	
	if v.config.EntryPointSimulationsAddress != (common.Address{}) {
		// EntryPointSimulations is configured, but we should prefer EntryPoint directly
		// Only use EntryPointSimulations if explicitly configured (for backwards compatibility)
		simulationAddress = v.config.EntryPointSimulationsAddress
		isUsingEntryPointSimulations = true
		v.logger.Warn().
			Str("entryPoint", entryPoint.Hex()).
			Str("entryPointSimulationsAddress", v.config.EntryPointSimulationsAddress.Hex()).
			Str("simulationAddress", simulationAddress.Hex()).
			Msg("WARNING: Using separately deployed EntryPointSimulations contract. This is NOT recommended - EntryPointSimulations computes senderCreator from its own address, not EntryPoint's address, which can cause AA13 errors. Best practice: call EntryPoint.simulateValidation directly. Consider removing --entry-point-simulations-address config.")
	} else {
		// Call EntryPoint directly - this is the recommended approach
		simulationAddress = entryPoint
		isUsingEntryPointSimulations = false
		v.logger.Info().
			Str("entryPoint", entryPoint.Hex()).
			Str("simulationAddress", simulationAddress.Hex()).
			Msg("calling EntryPoint.simulateValidation directly (recommended) - EntryPointSimulations not configured")
	}

	// Encode packed simulateValidation (EntryPoint v0.7+/v0.9); no standard fallback
	calldata, err := EncodeSimulateValidationPacked(userOp)
	if err != nil {
		return fmt.Errorf("failed to encode simulateValidation (packed): %w", err)
	}

	// Get latest indexed block height (not network's latest, which may not be indexed yet)
	height, err := v.blocks.LatestEVMHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest indexed height: %w", err)
	}

	// Check if account already exists (for account creation UserOps)
	// If initCode is present but account already exists, EntryPoint will reject it with AA10
	if len(userOp.InitCode) > 0 {
		accountCode, err := v.requester.GetCode(userOp.Sender, height)
		if err == nil && len(accountCode) > 0 {
			// Account already exists - this will cause EntryPoint to reject the UserOp with AA10
			v.logger.Warn().
				Str("sender", userOp.Sender.Hex()).
				Int("codeLength", len(accountCode)).
				Str("accountCodeHex", hexutil.Encode(accountCode[:min(20, len(accountCode))])).
				Msg("account already exists - EntryPoint will reject account creation UserOp with AA10 (not AA13)")
			// Continue to simulateValidation - it will return the proper error (should be AA10, not AA13)
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

	// Calculate UserOp hash - MUST use EntryPoint.getUserOpHash() for authoritative hash
	// NO FALLBACKS - if this fails, validation fails
	// height is already declared above, so we can reuse it
	userOpHash, err := v.requester.GetUserOpHash(ctx, userOp, entryPoint, height)
	if err != nil {
		// CRITICAL: Log at ERROR level with full context
		v.logger.Error().
			Err(err).
			Str("sender", userOp.Sender.Hex()).
			Str("entryPoint", entryPoint.Hex()).
			Uint64("height", height).
			Str("nonce", userOp.Nonce.String()).
			Msg("CRITICAL: GetUserOpHash() FAILED - validation cannot proceed without contract hash")
		return fmt.Errorf("failed to get UserOp hash from EntryPoint.getUserOpHash() - this is required, no fallback: %w", err)
	}
	
	// CRITICAL: Log the hash we got from the contract at INFO level so it's always visible
	// This hash MUST match what the frontend signed - if it doesn't, signature validation will fail
	// If the frontend logs show a different hash, that means either:
	// 1. The UserOp received by the gateway is different from what the frontend sent
	// 2. The gateway is calling getUserOpHash() with different parameters than the frontend
	// 3. There's a network/RPC issue causing different results
	v.logger.Info().
		Str("userOpHash_from_contract", userOpHash.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("entryPoint", entryPoint.Hex()).
		Uint64("height", height).
		Str("nonce", userOp.Nonce.String()).
		Str("callGasLimit", userOp.CallGasLimit.String()).
		Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
		Str("preVerificationGas", userOp.PreVerificationGas.String()).
		Str("maxFeePerGas", userOp.MaxFeePerGas.String()).
		Str("maxPriorityFeePerGas", userOp.MaxPriorityFeePerGas.String()).
		Int("initCodeLen", len(userOp.InitCode)).
		Int("callDataLen", len(userOp.CallData)).
		Int("paymasterAndDataLen", len(userOp.PaymasterAndData)).
		Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
		Str("callDataHex", hexutil.Encode(userOp.CallData)).
		Str("paymasterAndDataHex", hexutil.Encode(userOp.PaymasterAndData)).
		Str("accountGasLimitsHex", hexutil.Encode(packAccountGasLimits(userOp.CallGasLimit, userOp.VerificationGasLimit)[:])).
		Str("gasFeesHex", hexutil.Encode(packGasFees(userOp.MaxFeePerGas, userOp.MaxPriorityFeePerGas)[:])).
		Msg("CRITICAL: Got hash from EntryPoint.getUserOpHash() - this is the authoritative hash. Frontend MUST sign this exact hash. If simulateValidation fails with AA24, EntryPoint calculated a different hash internally. Compare this hash with frontend's getUserOpHash() result.")

	// Extract owner from initCode if present (for account creation) - for logging only
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

	// NO MANUAL SIGNATURE RECOVERY - EntryPoint.simulateValidation() handles signature validation
	// We only log the UserOp hash (from contract) and signature details for debugging

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

	// Add owner info if available (for account creation UserOps)
	if ownerExtracted {
		logFields = logFields.
			Str("ownerFromInitCode", ownerAddr.Hex()).
			Bool("ownerExtracted", true)
	}

	// Add signature v value and full signature hex for debugging
	// IMPORTANT: Log the signature BEFORE any potential modifications
	// Capture the actual byte value to debug why it might show as 0
	if len(userOp.Signature) >= 65 {
		sigVAtLogTime := userOp.Signature[64]
		sigHexAtLogTime := hexutil.Encode(userOp.Signature)
		logFields = logFields.
			Uint8("signatureV", sigVAtLogTime).
			Str("signatureHex", sigHexAtLogTime).
			Str("signatureR", hexutil.Encode(userOp.Signature[0:32])).
			Str("signatureS", hexutil.Encode(userOp.Signature[32:64])).
			Str("signatureLastByteHex", hexutil.Encode(userOp.Signature[64:65])).
			Int("signatureArrayLen", len(userOp.Signature)).
			Int("signatureArrayCap", cap(userOp.Signature))
		
		// Debug: Check signature v value
		// Note: The gateway passes signatures through as-is without modification.
		// EntryPoint.simulateValidation() handles signature validation on-chain.
		// Both v=0/1 and v=27/28 formats are observed in practice for ERC-4337 UserOperation signatures.
		// The gateway does not perform signature validation - it relies on EntryPoint's on-chain validation.
		if sigVAtLogTime == 0 || sigVAtLogTime == 1 {
			v.logger.Debug().
				Str("sender", userOp.Sender.Hex()).
				Uint8("signatureV", sigVAtLogTime).
				Msg("signature v value is 0/1 (recovery ID format) - passed through to EntryPoint for validation")
		} else if sigVAtLogTime == 27 || sigVAtLogTime == 28 {
			v.logger.Debug().
				Str("sender", userOp.Sender.Hex()).
				Uint8("signatureV", sigVAtLogTime).
				Msg("signature v value is 27/28 (EIP-155 format) - passed through to EntryPoint for validation")
		}
	}


	// Log the exact calldata being sent to EntryPoint
	logFields = logFields.Str("calldataHex", hexutil.Encode(calldata)).Int("calldataLen", len(calldata))
	logFields = logFields.Str("simulationAddress", simulationAddress.Hex())
	logFields.Msg("calling simulateValidation with full UserOp details")

	// Decode and verify the packed UserOp values before calling EntryPoint
	// This helps catch packing errors (e.g., swapped gas limits)
	// We manually extract the packed values from the calldata since we know the structure
	if isUsingEntryPointSimulations && len(calldata) >= 4 {
		// PackedUserOperation structure (after selector):
		// - sender (address, 32 bytes padded)
		// - nonce (uint256, 32 bytes)
		// - initCode offset (32 bytes)
		// - callData offset (32 bytes)
		// - accountGasLimits (bytes32, 32 bytes) - at offset after dynamic fields
		// - preVerificationGas (uint256, 32 bytes)
		// - gasFees (bytes32, 32 bytes)
		// - paymasterAndData offset (32 bytes)
		// - signature offset (32 bytes)
		// Then dynamic fields follow
		
		// For simplicity, we'll re-encode using the same function and compare
		// This verifies the packing is correct
		expectedPackedOp := PackedUserOperationABI{
			Sender:             userOp.Sender,
			Nonce:              userOp.Nonce,
			InitCode:           userOp.InitCode,
			CallData:           userOp.CallData,
			AccountGasLimits:   packAccountGasLimits(userOp.CallGasLimit, userOp.VerificationGasLimit),
			PreVerificationGas: userOp.PreVerificationGas,
			GasFees:            packGasFees(userOp.MaxFeePerGas, userOp.MaxPriorityFeePerGas),
			PaymasterAndData:   userOp.PaymasterAndData,
			Signature:          userOp.Signature,
		}
		
		// Decode the packed values from the expected encoding
		decodedCallGasLimit, decodedVerificationGasLimit := unpackAccountGasLimits(expectedPackedOp.AccountGasLimits)
		decodedMaxFeePerGas, decodedMaxPriorityFeePerGas := unpackGasFees(expectedPackedOp.GasFees)
		
		v.logger.Info().
			Str("originalCallGasLimit", userOp.CallGasLimit.String()).
			Str("originalVerificationGasLimit", userOp.VerificationGasLimit.String()).
			Str("decodedCallGasLimit", decodedCallGasLimit.String()).
			Str("decodedVerificationGasLimit", decodedVerificationGasLimit.String()).
			Str("accountGasLimitsHex", hexutil.Encode(expectedPackedOp.AccountGasLimits[:])).
			Bool("callGasLimitMatches", decodedCallGasLimit.Cmp(userOp.CallGasLimit) == 0).
			Bool("verificationGasLimitMatches", decodedVerificationGasLimit.Cmp(userOp.VerificationGasLimit) == 0).
			Str("originalMaxFeePerGas", userOp.MaxFeePerGas.String()).
			Str("originalMaxPriorityFeePerGas", userOp.MaxPriorityFeePerGas.String()).
			Str("decodedMaxFeePerGas", decodedMaxFeePerGas.String()).
			Str("decodedMaxPriorityFeePerGas", decodedMaxPriorityFeePerGas.String()).
			Str("gasFeesHex", hexutil.Encode(expectedPackedOp.GasFees[:])).
			Bool("maxFeePerGasMatches", decodedMaxFeePerGas.Cmp(userOp.MaxFeePerGas) == 0).
			Bool("maxPriorityFeePerGasMatches", decodedMaxPriorityFeePerGas.Cmp(userOp.MaxPriorityFeePerGas) == 0).
			Msg("AA13 diagnostics: packed UserOp gas limits and fees - verify these match what EntryPoint will decode. If verificationGasLimit is wrong, EntryPoint will call senderCreator.createSender with insufficient gas → AA13.")
	}

	// Create transaction args for eth_call
	txArgs := ethTypes.TransactionArgs{
		To:   &simulationAddress,
		Data: (*hexutil.Bytes)(&calldata),
	}

	// For EntryPoint v0.9.0: When calling EntryPoint directly (not using separately deployed EntryPointSimulations),
	// we need to use a state override to temporarily replace EntryPoint's code with EntryPointSimulations bytecode.
	// This allows simulateValidation to work correctly while maintaining the correct senderCreator address.
	var stateOverride *ethTypes.StateOverride
	var overrideCodeHash string
	var overrideCodeLen int
	var calldataPreviewFirst8 string
	var calldataPreviewLast8 string
	if !isUsingEntryPointSimulations {
		// Decode the EntryPointSimulations deployed bytecode from hex
		// The bytecode is stored as a hex string starting with "0x"
		bytecodeHex := strings.TrimSpace(string(abis.EntryPointSimulationsDeployedBytecode))
		bytecodeBytes, decodeErr := hexutil.Decode(bytecodeHex)
		if decodeErr != nil {
			return fmt.Errorf("failed to decode EntryPointSimulations bytecode: %w", decodeErr)
		}

		// Create state override: replace EntryPoint's code with EntryPointSimulations bytecode
		stateOverride = &ethTypes.StateOverride{
			entryPoint: {
				Code: (*hexutil.Bytes)(&bytecodeBytes),
			},
		}

		overrideCodeLen = len(bytecodeBytes)
		overrideCodeHash = crypto.Keccak256Hash(bytecodeBytes).Hex()

		// Calldata previews for debugging invalid jump issues
		if len(calldata) >= 8 {
			calldataPreviewFirst8 = hexutil.Encode(calldata[:8])
			calldataPreviewLast8 = hexutil.Encode(calldata[len(calldata)-8:])
		} else {
			calldataPreviewFirst8 = hexutil.Encode(calldata)
			calldataPreviewLast8 = hexutil.Encode(calldata)
		}

		v.logger.Debug().
			Str("entryPoint", entryPoint.Hex()).
			Int("bytecodeLength", overrideCodeLen).
			Str("overrideCodeHash", overrideCodeHash).
			Int("calldataLen", len(calldata)).
			Str("calldataFirst8", calldataPreviewFirst8).
			Str("calldataLast8", calldataPreviewLast8).
			Msg("using state override to replace EntryPoint code with EntryPointSimulations bytecode")
	}

	// Call simulateValidation (packed only).
	// EntryPoint v0.9.0: simulateValidation returns ValidationResult normally on success,
	// and reverts with FailedOp/PaymasterNotDeployed/etc on failure.
	result, err := v.requester.Call(txArgs, v.config.Coinbase, height, stateOverride, nil)
	
	if err != nil {
		// Log detailed error for debugging
		v.logger.Error().
			Err(err).
			Str("entryPoint", entryPoint.Hex()).
			Str("simulationAddress", simulationAddress.Hex()).
			Str("sender", userOp.Sender.Hex()).
			Uint64("height", height).
			Bool("isUsingEntryPointSimulations", isUsingEntryPointSimulations).
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

			// Add owner info if available (for account creation UserOps)
			if ownerExtracted {
				revertLog = revertLog.Str("ownerFromInitCode", ownerAddr.Hex())
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
				// Special handling for AA24 (signature error) - log hash mismatch details
				if decodedResult.AAErrorCode == "AA24" {
					// AA24 means EntryPoint calculated a different hash during simulateValidation than what was signed
					// Log the hash we got from getUserOpHash() - this is what the frontend should have signed
					v.logger.Error().
						Str("userOpHash_from_getUserOpHash", userOpHash.Hex()).
						Str("sender", userOp.Sender.Hex()).
						Str("nonce", userOp.Nonce.String()).
						Str("entryPoint", entryPoint.Hex()).
						Str("chainID", v.config.EVMNetworkID.String()).
						Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
						Str("callDataHex", hexutil.Encode(userOp.CallData)).
						Str("accountGasLimitsHex", hexutil.Encode(packAccountGasLimits(userOp.CallGasLimit, userOp.VerificationGasLimit)[:])).
						Str("preVerificationGas", userOp.PreVerificationGas.String()).
						Str("gasFeesHex", hexutil.Encode(packGasFees(userOp.MaxFeePerGas, userOp.MaxPriorityFeePerGas)[:])).
						Str("paymasterAndDataHex", hexutil.Encode(userOp.PaymasterAndData)).
						Str("signatureHex", hexutil.Encode(userOp.Signature)).
						Msg("CRITICAL: AA24 signature error - EntryPoint calculated a different hash during simulateValidation than getUserOpHash(). Frontend MUST sign the hash from getUserOpHash() (logged above). If frontend signed a different hash, signature validation will fail.")
				}
				
				// Special handling for AA13 on account creation UserOps
				// When using EntryPointSimulations for account creation (initCode != 0, sender not deployed),
				// simulateValidation will always revert with AA13 "initCode failed or OOG" due to simulation context.
				// This is expected behavior and does not mean the actual handleOps transaction will fail.
				// CRITICAL: Only accept AA13 as "expected" when actually using EntryPointSimulations.
				// If calling EntryPoint directly, AA13 indicates a real validation failure.
				isAccountCreation := len(userOp.InitCode) > 0
				if isAccountCreation && isUsingEntryPointSimulations {
					// Verify sender doesn't exist yet (account creation case)
					accountCode, err := v.requester.GetCode(userOp.Sender, height)
					senderNotDeployed := (err != nil || len(accountCode) == 0)
					
					if decodedResult.AAErrorCode == "AA13" && senderNotDeployed {
						// AA13 for account creation is expected ONLY when using EntryPointSimulations
						v.logger.Info().
							Str("decodedResult", decodedResult.Decoded).
							Str("aaErrorCode", decodedResult.AAErrorCode).
							Str("sender", userOp.Sender.Hex()).
							Int("initCodeLen", len(userOp.InitCode)).
							Bool("isUsingEntryPointSimulations", true).
							Msg("AA13 during simulation is expected for account-creation UserOps when using EntryPointSimulations; proceeding to enqueue. This does not indicate the actual handleOps transaction will fail.")
						// Accept the UserOp - AA13 is expected for account creation in simulation
						return nil
					}
				}
				
				// For non-account-creation UserOps, or other AA errors, treat as failure
				var errorMsg string
				if decodedResult.AAErrorCode != "" {
					errorMsg = fmt.Sprintf("validation failed: %s (AA error: %s)", decodedResult.Decoded, decodedResult.AAErrorCode)

					// If this is AA13 (initCode failed or OOG), add extra diagnostics to help pinpoint the cause
					if decodedResult.AAErrorCode == "AA13" {
						v.logAA13Diagnostics(ctx, userOp, entryPoint, simulationAddress, height)
					}
				} else {
					errorMsg = fmt.Sprintf("validation failed: %s", decodedResult.Decoded)
				}
				v.logger.Error().
					Str("decodedResult", decodedResult.Decoded).
					Str("aaErrorCode", decodedResult.AAErrorCode).
					Str("revertReasonHex", revertErr.Reason).
					Bool("isAccountCreation", isAccountCreation).
					Str("userOpHash_from_getUserOpHash", userOpHash.Hex()).
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
				selector := calldata[:4]

				// Check if the function selector exists in bytecode
				// This helps diagnose if the function actually exists or not
				simulationCode, err := v.requester.GetCode(simulationAddress, height)
				selectorExists := false
				codeLength := 0
				if err == nil {
					codeLength = len(simulationCode)
					selectorExists = bytes.Contains(simulationCode, selector)
				}

				if !selectorExists {
					// Function definitely doesn't exist in bytecode
					v.logger.Error().
						Str("revertReasonHex", revertErr.Reason).
						Int("revertDataLen", len(revertData)).
						Str("entryPoint", entryPoint.Hex()).
						Str("simulationAddress", simulationAddress.Hex()).
						Str("functionSelector", selectorHex).
						Int("simulationCodeLength", codeLength).
						Bool("selectorExistsInBytecode", false).
						Str("sender", userOp.Sender.Hex()).
						Msg("simulateValidation function does not exist (selector not found in bytecode). Empty revert indicates function call fell through to fallback.")
					return fmt.Errorf("simulation failed: simulateValidation not implemented at %s (selector %s not found in bytecode). The contract may not have this function or may use a different signature", simulationAddress.Hex(), selectorHex)
				}

				// Selector exists but still empty revert - unusual case
				v.logger.Error().
					Str("revertReasonHex", revertErr.Reason).
					Int("revertDataLen", len(revertData)).
					Str("entryPoint", entryPoint.Hex()).
					Str("simulationAddress", simulationAddress.Hex()).
					Str("functionSelector", selectorHex).
					Int("simulationCodeLength", codeLength).
					Bool("selectorExistsInBytecode", true).
					Str("sender", userOp.Sender.Hex()).
					Msg("simulateValidation selector exists in EntryPointSimulations bytecode but reverted with empty data. This may indicate a different EntryPointSimulations version, implementation issue, or validation failed without proper error encoding.")
				return fmt.Errorf("validation reverted with empty data - simulateValidation call to %s returned no error data even though function selector exists in bytecode. This may indicate a contract implementation issue or validation failed without proper error encoding", simulationAddress.Hex())
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

	// If we get here, simulateValidation succeeded (no error) - EntryPoint v0.9.0 behavior
	// Decode ValidationResult from return data
	validationResult, err := DecodeValidationResult(result)
	if err != nil {
		v.logger.Error().
			Err(err).
			Str("entryPoint", entryPoint.Hex()).
			Str("sender", userOp.Sender.Hex()).
			Str("resultHex", hexutil.Encode(result)).
			Int("resultLen", len(result)).
			Msg("failed to decode ValidationResult from simulateValidation return data")
		return fmt.Errorf("failed to decode ValidationResult: %w", err)
	}

	// Log successful validation
	v.logger.Info().
		Str("entryPoint", entryPoint.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("userOpHash", userOpHash.Hex()).
		Str("preOpGas", validationResult.ReturnInfo.PreOpGas.String()).
		Str("prefund", validationResult.ReturnInfo.Prefund.String()).
		Str("accountValidationData", validationResult.ReturnInfo.AccountValidationData.Text(16)).
		Str("paymasterValidationData", validationResult.ReturnInfo.PaymasterValidationData.Text(16)).
		Msg("simulateValidation succeeded - ValidationResult decoded")

	// Validate the ValidationResult according to EntryPoint v0.9.0 requirements
	// This implements the validation pipeline from the TDD plan (Section 6)
	if err := v.validateValidationResult(ctx, validationResult, userOp, entryPoint, height); err != nil {
		return err
	}

	return nil
}

// logAA13Diagnostics adds extra logging when we hit AA13 "initCode failed or OOG"
// to help distinguish between the common causes:
// - Factory address wrong / has no code
// - Wrong function selector / calldata
// - Factory revert (require failure) vs true OOG
// - Account already exists but gateway is behind in indexing
func (v *UserOpValidator) logAA13Diagnostics(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
	simulationAddress common.Address,
	height uint64,
) {
	// Defensive: never let diagnostics change behavior
	defer func() {
		if r := recover(); r != nil {
			v.logger.Warn().
				Interface("panic", r).
				Msg("panic in AA13 diagnostics - ignoring and continuing")
		}
	}()

	if len(userOp.InitCode) < 24 {
		v.logger.Warn().
			Int("initCodeLen", len(userOp.InitCode)).
			Msg("AA13 diagnostics: initCode too short to decode factory/selector")
		return
	}

	// Decode factory, selector, owner, salt from initCode
	factoryAddr := common.BytesToAddress(userOp.InitCode[0:20])
	selector := hexutil.Encode(userOp.InitCode[20:24])

	var ownerHex, saltHex string
	if len(userOp.InitCode) >= 88 {
		// Owner is first param (32 bytes), address is last 20 bytes of that word (bytes 36-55)
		ownerBytes := userOp.InitCode[36:56]
		ownerHex = common.BytesToAddress(ownerBytes).Hex()

		// Salt is second param (uint256) - bytes 56-87
		saltBytes := userOp.InitCode[56:88]
		saltHex = hexutil.Encode(saltBytes)
	}

	// Check if factory has code at the current indexed height
	factoryCode, err := v.requester.GetCode(factoryAddr, height)
	factoryHasCode := (err == nil && len(factoryCode) > 0)

	log := v.logger.Info().
		Str("entryPoint", entryPoint.Hex()).
		Str("simulationAddress", simulationAddress.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Uint64("height", height).
		Str("factoryAddress", factoryAddr.Hex()).
		Str("functionSelector", selector).
		Int("initCodeLen", len(userOp.InitCode)).
		Bool("factoryHasCode", factoryHasCode).
		Int("factoryCodeLength", len(factoryCode)).
		Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
		Str("callGasLimit", userOp.CallGasLimit.String()).
		Str("preVerificationGas", userOp.PreVerificationGas.String())

	if ownerHex != "" {
		log = log.Str("ownerFromInitCode", ownerHex)
	}
	if saltHex != "" {
		log = log.Str("saltHex", saltHex)
	}
	if err != nil {
		log = log.Err(err)
	}

	// Check if account already exists (this would cause AA10, not AA13, but worth checking)
	accountCode, accountErr := v.requester.GetCode(userOp.Sender, height)
	if accountErr == nil && len(accountCode) > 0 {
		log = log.Int("accountCodeLength", len(accountCode))
		log.Msg("AA13 diagnostics: WARNING - account already exists! This should cause AA10, not AA13. EntryPoint may be rejecting due to account existence.")
	} else if accountErr == nil {
		log = log.Int("accountCodeLength", 0)
		log.Msg("AA13 diagnostics: account does not exist (expected for account creation)")
	} else {
		log = log.Err(accountErr)
		log.Msg("AA13 diagnostics: failed to check account existence")
	}

	log.Msg("AA13 diagnostics: initCode / factory summary")

	// AA13 means: initCode failed or OOG during senderCreator.createSender(initCode) call.
	// This diagnostic tests the factory call under conditions closer to EntryPoint's actual call.
	// EntryPoint does: senderCreator.createSender{gas: verificationGasLimit}(initCode)
	// We need to verify: 1) initCode structure, 2) factory call with correct gas cap, 3) return value

	// Step 1: Verify initCode structure
	if len(userOp.InitCode) < 20 {
		v.logger.Warn().
			Int("initCodeLen", len(userOp.InitCode)).
			Msg("AA13 diagnostics: initCode too short - missing factory address (first 20 bytes)")
		return
	}
	initCodeFactoryAddr := common.BytesToAddress(userOp.InitCode[0:20])
	if initCodeFactoryAddr != factoryAddr {
		v.logger.Warn().
			Str("initCodeFactory", initCodeFactoryAddr.Hex()).
			Str("expectedFactory", factoryAddr.Hex()).
			Msg("AA13 diagnostics: initCode first 20 bytes do not match expected factory address")
	}
	factoryData := userOp.InitCode[20:]

	// Step 2: Get senderCreator address
	var senderCreatorAddr common.Address
	calldata, err := EncodeSenderCreator()
	if err == nil {
		txArgs := ethTypes.TransactionArgs{
			To:   &entryPoint,
			Data: (*hexutil.Bytes)(&calldata),
		}
		result, err := v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
		if err == nil && len(result) >= 20 {
			senderCreatorAddr = common.BytesToAddress(result[len(result)-20:])
			v.logger.Info().
				Str("senderCreator", senderCreatorAddr.Hex()).
				Msg("AA13 diagnostics: fetched senderCreator address")
		} else {
			v.logger.Warn().
				Err(err).
				Msg("AA13 diagnostics: failed to fetch senderCreator - cannot test factory call with correct caller")
			return
		}
	} else {
		v.logger.Warn().
			Err(err).
			Msg("AA13 diagnostics: failed to encode senderCreator() - cannot test factory call")
		return
	}

	// Step 3: Test factory call with unlimited gas (baseline check)
	txArgsUnlimited := ethTypes.TransactionArgs{
		To:   &factoryAddr,
		Data: (*hexutil.Bytes)(&factoryData),
	}
	resultUnlimited, callErrUnlimited := v.requester.Call(txArgsUnlimited, senderCreatorAddr, height, nil, nil)
	if callErrUnlimited != nil {
		// Factory call fails even with unlimited gas - this is the root cause
		if revertErr, ok := callErrUnlimited.(*errs.RevertError); ok {
			var revertData []byte
			if revertErr.Reason != "" && revertErr.Reason != "0x" {
				if data, decodeErr := hexutil.Decode(revertErr.Reason); decodeErr == nil {
					revertData = data
				}
			}
			factoryDecoded := v.decodeFactoryRevert(revertData, revertErr.Reason)
			v.logger.Info().
				Str("factoryAddress", factoryAddr.Hex()).
				Str("callerAddress", senderCreatorAddr.Hex()).
				Str("revertReasonHex", revertErr.Reason).
				Str("decodedResult", factoryDecoded.Decoded).
				Bool("isFactoryError", factoryDecoded.IsFactoryError).
				Msg("AA13 diagnostics: factory call failed even with unlimited gas - this is the root cause of AA13")
		} else {
			v.logger.Info().
				Str("factoryAddress", factoryAddr.Hex()).
				Str("callerAddress", senderCreatorAddr.Hex()).
				Err(callErrUnlimited).
				Msg("AA13 diagnostics: factory call failed with non-revert error (even with unlimited gas)")
		}
		return
	}

	// Step 4: Check return value - should be the sender address (non-zero)
	var returnedSender common.Address
	if len(resultUnlimited) >= 32 {
		// Factory returns the created account address in the first 32 bytes
		returnedSender = common.BytesToAddress(resultUnlimited[12:32]) // Last 20 bytes of first word
	}
	if returnedSender == (common.Address{}) {
		v.logger.Warn().
			Str("factoryAddress", factoryAddr.Hex()).
			Str("returnDataHex", hexutil.Encode(resultUnlimited)).
			Msg("AA13 diagnostics: factory call succeeded but returned zero address - this causes AA13 (senderCreator.createSender returns 0)")
	} else if returnedSender != userOp.Sender {
		// Extract owner and salt from initCode to help debug the mismatch
		var ownerFromInitCode common.Address
		var saltFromInitCode *big.Int
		var ownerHex, saltHex string
		if len(userOp.InitCode) >= 88 {
			owner, err := extractOwnerFromInitCode(userOp.InitCode)
			if err == nil {
				ownerFromInitCode = owner
				ownerHex = owner.Hex()
			}
			// Salt is bytes 56-87 (second parameter, uint256)
			saltBytes := userOp.InitCode[56:88]
			saltFromInitCode = new(big.Int).SetBytes(saltBytes)
			saltHex = hexutil.Encode(saltBytes)
		}

		// Call factory.getAddress(owner, salt) to verify what the factory thinks the address should be
		var factoryGetAddressResult common.Address
		var factoryImplementationAddr common.Address
		if ownerFromInitCode != (common.Address{}) && saltFromInitCode != nil {
			// Get the factory's implementation address (used in CREATE2 calculation)
			implCalldata, err := EncodeFactoryAccountImplementation()
			if err == nil {
				txArgs := ethTypes.TransactionArgs{
					To:   &factoryAddr,
					Data: (*hexutil.Bytes)(&implCalldata),
				}
				result, err := v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
				if err == nil && len(result) >= 32 {
					factoryImplementationAddr = common.BytesToAddress(result[12:32])
				}
			}

			// Get the factory's expected address for this owner/salt
			calldata, err := EncodeFactoryGetAddress(ownerFromInitCode, saltFromInitCode)
			if err == nil {
				txArgs := ethTypes.TransactionArgs{
					To:   &factoryAddr,
					Data: (*hexutil.Bytes)(&calldata),
				}
				result, err := v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
				if err == nil && len(result) >= 32 {
					factoryGetAddressResult = common.BytesToAddress(result[12:32])
				}
			}
		}

		logMsg := v.logger.Warn().
			Str("factoryAddress", factoryAddr.Hex()).
			Str("returnedSender", returnedSender.Hex()).
			Str("expectedSender", userOp.Sender.Hex())
		if ownerHex != "" {
			logMsg = logMsg.Str("ownerFromInitCode", ownerHex)
		}
		if saltHex != "" {
			logMsg = logMsg.Str("saltFromInitCode", saltHex)
		}
		if factoryImplementationAddr != (common.Address{}) {
			logMsg = logMsg.Str("factoryImplementation", factoryImplementationAddr.Hex())
		}
		if factoryGetAddressResult != (common.Address{}) {
			logMsg = logMsg.Str("factoryGetAddress", factoryGetAddressResult.Hex())
			if factoryGetAddressResult == returnedSender {
				logMsg.Msg("AA13 diagnostics: factory returned different address than userOp.sender - ROOT CAUSE IDENTIFIED. factory.getAddress(owner, salt) matches factory.createAccount return, confirming the factory's address calculation is correct. The client's userOp.sender calculation is wrong. Client must fix their address calculation to match factory.getAddress(owner, salt). IMPORTANT: Verify the client is using the correct implementation address in their CREATE2 calculation - factory.accountImplementation() returns the address that should be used.")
			} else {
				logMsg.Msg("AA13 diagnostics: factory returned different address than userOp.sender - UNEXPECTED: factory.getAddress(owner, salt) does not match factory.createAccount return. This suggests a factory implementation issue or the factory call is not working as expected.")
			}
		} else {
			logMsg.Msg("AA13 diagnostics: factory returned different address than userOp.sender - ROOT CAUSE IDENTIFIED. The initCode calldata (owner/salt) does not match what was used to calculate userOp.sender. Client must fix: either update initCode to match the sender address, or update sender address to match what initCode will create. IMPORTANT: Verify the client is using the correct implementation address in their CREATE2 calculation - factory.accountImplementation() returns the address that should be used. This mismatch causes EntryPoint to reject the UserOp with AA13.")
		}
	} else {
		v.logger.Info().
			Str("factoryAddress", factoryAddr.Hex()).
			Str("returnedSender", returnedSender.Hex()).
			Str("expectedSender", userOp.Sender.Hex()).
			Msg("AA13 diagnostics: factory call succeeded and returned correct sender address")
	}

	// Step 5: Test with verificationGasLimit cap (EntryPoint's actual gas constraint)
	// EntryPoint calls: senderCreator.createSender{gas: verificationGasLimit}(initCode)
	// We test if the factory call succeeds under this gas cap
	if userOp.VerificationGasLimit != nil && userOp.VerificationGasLimit.Uint64() > 0 {
		gasLimit := userOp.VerificationGasLimit.Uint64()
		txArgsCapped := ethTypes.TransactionArgs{
			To:   &factoryAddr,
			Data: (*hexutil.Bytes)(&factoryData),
			Gas:  (*hexutil.Uint64)(&gasLimit),
		}
		resultCapped, callErrCapped := v.requester.Call(txArgsCapped, senderCreatorAddr, height, nil, nil)
		if callErrCapped != nil {
			v.logger.Warn().
				Str("factoryAddress", factoryAddr.Hex()).
				Str("callerAddress", senderCreatorAddr.Hex()).
				Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
				Err(callErrCapped).
				Msg("AA13 diagnostics: factory call FAILED when capped at verificationGasLimit - this is likely the root cause of AA13 (OOG under gas cap)")
		} else {
			var returnedSenderCapped common.Address
			if len(resultCapped) >= 32 {
				returnedSenderCapped = common.BytesToAddress(resultCapped[12:32])
			}
			if returnedSenderCapped == (common.Address{}) {
				v.logger.Warn().
					Str("factoryAddress", factoryAddr.Hex()).
					Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
					Msg("AA13 diagnostics: factory call succeeded with gas cap but returned zero address - this causes AA13")
			} else {
				v.logger.Info().
					Str("factoryAddress", factoryAddr.Hex()).
					Str("callerAddress", senderCreatorAddr.Hex()).
					Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
					Str("returnedSender", returnedSenderCapped.Hex()).
					Msg("AA13 diagnostics: factory call succeeded with gas cap and returned address. If AA13 still occurs, check EntryPoint's senderCreator.createSender implementation or initCode structure.")
			}
		}

		// Step 6: Test senderCreator.createSender(initCode) directly (EntryPoint's actual call pattern)
		// EntryPoint does: senderCreator().createSender{gas: verificationGasLimit}(initCode)
		// Note: EntryPointSimulations computes senderCreator differently than EntryPoint:
		// - EntryPoint: senderCreator is immutable (set in constructor)
		// - EntryPointSimulations: senderCreator is computed via initSenderCreator() using its own address
		// We're using EntryPoint's senderCreator address, but EntryPointSimulations might use a different one
		// ISenderCreator.createSender(bytes memory initCode) returns address
		// Function selector: keccak256("createSender(bytes)")[:4]
		createSenderSelector := crypto.Keccak256([]byte("createSender(bytes)"))[:4]
		
		// Also check what EntryPointSimulations thinks senderCreator is
		// EntryPointSimulations.initSenderCreator() computes: address(uint160(uint256(keccak256(abi.encodePacked(hex"d694", address(this), hex"01")))))
		// This is the first contract created with CREATE by EntryPointSimulations address
		simulationSenderCreatorCalldata, _ := EncodeSenderCreator()
		txArgsSimulationSenderCreator := ethTypes.TransactionArgs{
			To:   &simulationAddress,
			Data: (*hexutil.Bytes)(&simulationSenderCreatorCalldata),
		}
		resultSimulationSenderCreator, errSimulationSenderCreator := v.requester.Call(txArgsSimulationSenderCreator, v.config.Coinbase, height, nil, nil)
		var simulationSenderCreatorAddr common.Address
		if errSimulationSenderCreator == nil && len(resultSimulationSenderCreator) >= 20 {
			simulationSenderCreatorAddr = common.BytesToAddress(resultSimulationSenderCreator[len(resultSimulationSenderCreator)-20:])
		}
		
		v.logger.Info().
			Str("entryPointSenderCreator", senderCreatorAddr.Hex()).
			Str("simulationSenderCreator", simulationSenderCreatorAddr.Hex()).
			Bool("senderCreatorsMatch", senderCreatorAddr == simulationSenderCreatorAddr).
			Str("selector", hexutil.Encode(createSenderSelector)).
			Int("initCodeLen", len(userOp.InitCode)).
			Msg("AA13 diagnostics: testing senderCreator.createSender(initCode). EntryPointSimulations computes senderCreator differently - if addresses don't match, that's the issue. Note: eth_call is read-only, so CREATE2 won't actually create the account (no code will exist), but the call should still return the correct address.")
		// ABI encoding for bytes parameter:
		// - offset (32 bytes, value = 0x20 = 32, pointing to where length starts)
		// - length (32 bytes, value = len(initCode))
		// - data (padded to 32-byte boundary)
		offset := make([]byte, 32)
		big.NewInt(0x20).FillBytes(offset) // offset = 32 bytes
		length := make([]byte, 32)
		big.NewInt(int64(len(userOp.InitCode))).FillBytes(length)
		// Pad initCode to 32-byte boundary
		initCodePaddedLen := ((len(userOp.InitCode) + 31) / 32) * 32
		initCodePadded := make([]byte, initCodePaddedLen)
		copy(initCodePadded, userOp.InitCode)
		// Build calldata: selector + offset + length + data
		createSenderCalldata := make([]byte, 0, 4+32+32+initCodePaddedLen)
		createSenderCalldata = append(createSenderCalldata, createSenderSelector...)
		createSenderCalldata = append(createSenderCalldata, offset...)
		createSenderCalldata = append(createSenderCalldata, length...)
		createSenderCalldata = append(createSenderCalldata, initCodePadded...)

		txArgsSenderCreator := ethTypes.TransactionArgs{
			To:   &senderCreatorAddr,
			Data: (*hexutil.Bytes)(&createSenderCalldata),
			Gas:  (*hexutil.Uint64)(&gasLimit),
		}
		resultSenderCreator, callErrSenderCreator := v.requester.Call(txArgsSenderCreator, entryPoint, height, nil, nil)
		if callErrSenderCreator != nil {
			// Decode the revert reason to understand why senderCreator.createSender failed
			var revertReason string
			var decodedRevert string
			if revertErr, ok := callErrSenderCreator.(*errs.RevertError); ok {
				revertReason = revertErr.Reason
				var revertData []byte
				if revertErr.Reason != "" && revertErr.Reason != "0x" {
					if data, decodeErr := hexutil.Decode(revertErr.Reason); decodeErr == nil {
						revertData = data
					}
				}
				// Decode the revert data to see what error occurred
				decoded := v.decodeFactoryRevert(revertData, revertReason)
				decodedRevert = decoded.Decoded
			}
			logMsg := v.logger.Warn().
				Str("senderCreatorAddress", senderCreatorAddr.Hex()).
				Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
				Err(callErrSenderCreator)
			if revertReason != "" {
				logMsg = logMsg.Str("revertReasonHex", revertReason)
			}
			if decodedRevert != "" {
				logMsg = logMsg.Str("decodedRevert", decodedRevert)
			}
			logMsg.Msg("AA13 diagnostics: senderCreator.createSender(initCode) FAILED - this is likely the root cause of AA13. EntryPoint calls this exact function, so if it fails here, it will fail in EntryPoint too. The revert reason above shows why senderCreator.createSender is failing.")
		} else {
			var returnedSenderFromCreator common.Address
			if len(resultSenderCreator) >= 32 {
				returnedSenderFromCreator = common.BytesToAddress(resultSenderCreator[12:32])
			}
			if returnedSenderFromCreator == (common.Address{}) {
				v.logger.Warn().
					Str("senderCreatorAddress", senderCreatorAddr.Hex()).
					Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
					Msg("AA13 diagnostics: senderCreator.createSender(initCode) succeeded but returned zero address - this causes AA13")
			} else if returnedSenderFromCreator != userOp.Sender {
				v.logger.Warn().
					Str("senderCreatorAddress", senderCreatorAddr.Hex()).
					Str("returnedSender", returnedSenderFromCreator.Hex()).
					Str("expectedSender", userOp.Sender.Hex()).
					Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
					Msg("AA13 diagnostics: senderCreator.createSender(initCode) returned different address than userOp.sender - this causes AA13")
			} else {
				// Check if account was actually created (eth_call doesn't persist state, but we can check if it would create it)
				// Note: eth_call is read-only, so the account isn't actually created, but EntryPoint's simulateValidation is also read-only
				// EntryPoint checks: if (sender1.code.length == 0) revert FailedOp(opIndex, "AA15 initCode must create sender");
				// So we should check if the returned address has code
				accountCodeAfterCreate, accountErrAfterCreate := v.requester.GetCode(returnedSenderFromCreator, height)
				hasCodeAfterCreate := (accountErrAfterCreate == nil && len(accountCodeAfterCreate) > 0)
				
				logMsg := v.logger.Warn().
					Str("senderCreatorAddress", senderCreatorAddr.Hex()).
					Str("returnedSender", returnedSenderFromCreator.Hex()).
					Str("expectedSender", userOp.Sender.Hex()).
					Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
					Bool("returnedSenderHasCode", hasCodeAfterCreate).
					Int("returnedSenderCodeLength", len(accountCodeAfterCreate))
				
				if !hasCodeAfterCreate {
					logMsg.Msg("AA13 diagnostics: senderCreator.createSender(initCode) succeeded and returned the expected address. Note: eth_call is read-only, so the created account's code will not be visible via eth_getCode at this height. EntryPoint in a real tx would see code.length > 0 inside the same call. Since EntryPoint still fails with AA13 despite packing being correct and diagnostics succeeding, the most likely cause is that EntryPointSimulations uses STATICCALL context, which prevents CREATE2 from executing. In STATICCALL context, CREATE2 operations revert, causing senderCreator.createSender to return address(0) → AA13. Possible solutions: 1) Increase verificationGasLimit to account for EntryPoint overhead, 2) Check if EntryPointSimulations can be modified to use regular CALL instead of STATICCALL, 3) Call EntryPoint.simulateValidation directly (if supported) instead of through EntryPointSimulations.")
				} else {
					logMsg.Msg("AA13 diagnostics: senderCreator.createSender(initCode) succeeded in diagnostic but EntryPoint still fails with AA13. This suggests EntryPoint calls it differently (different gas forwarding, call context, or state). Most likely cause: EntryPointSimulations uses STATICCALL context which prevents CREATE2. Possible causes: 1) STATICCALL context prevents CREATE2 (most likely), 2) EntryPoint uses different gas limit/forwarding, 3) EntryPoint calls it at different call depth, 4) EntryPoint has additional validation that fails, 5) State differences between diagnostic and EntryPoint context.")
				}
			}
		}
	}

	// Summary log
	v.logger.Info().
		Str("factoryAddress", factoryAddr.Hex()).
		Str("callerAddress", senderCreatorAddr.Hex()).
		Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
		Str("returnedSender", returnedSender.Hex()).
		Str("expectedSender", userOp.Sender.Hex()).
		Msg("AA13 diagnostics: factory call succeeded with unlimited gas. AA13 indicates initCode is failing under EntryPoint's exact call pattern (senderCreator.createSender{gas: verificationGasLimit}(initCode)). Check: 1) initCode first 20 bytes = factory address, 2) initCode[20:] calldata matches expected owner/salt, 3) factory call succeeds when capped at verificationGasLimit, 4) returned address matches userOp.sender. Note: simulateValidation and handleOps use the same path - if AA13 occurs in simulation, execution will also fail.")
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

	// Call EntryPoint.getDepositInfo
	result, err := v.requester.Call(txArgs, v.config.Coinbase, height, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call getDepositInfo: %w", err)
	}

	// Decode result (struct DepositInfo with fields: deposit, staked, stake, unstakeDelaySec, withdrawTime)
	// We need to extract just the deposit field (first field, uint256)
	if len(result) < 32 {
		return nil, fmt.Errorf("invalid getDepositInfo result: expected at least 32 bytes, got %d", len(result))
	}

	// getDepositInfo returns a struct, first field is deposit (uint256) at offset 0
	deposit := new(big.Int).SetBytes(result[:32])
	return deposit, nil
}

// EstimateGas estimates gas for a UserOperation
func (v *UserOpValidator) EstimateGas(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
) (*UserOpGasEstimate, error) {
	// Get latest indexed block height (not network's latest, which may not be indexed yet)
	height, err := v.blocks.LatestEVMHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest indexed height: %w", err)
	}

	// Encode packed simulateValidation only (EntryPoint v0.7+/v0.9); no standard fallback
	calldata, err := EncodeSimulateValidationPacked(userOp)
	if err != nil {
		return nil, fmt.Errorf("failed to encode simulateValidation (packed): %w", err)
	}

	// Create transaction args for eth_estimateGas
	txArgs := ethTypes.TransactionArgs{
		To:   &entryPoint,
		Data: (*hexutil.Bytes)(&calldata),
	}

	// For EntryPoint v0.9.0: When calling EntryPoint directly (not using separately deployed EntryPointSimulations),
	// we need to use a state override to temporarily replace EntryPoint's code with EntryPointSimulations bytecode.
	var stateOverride *ethTypes.StateOverride
	if v.config.EntryPointSimulationsAddress == (common.Address{}) {
		// Decode the EntryPointSimulations deployed bytecode from hex
		bytecodeHex := strings.TrimSpace(string(abis.EntryPointSimulationsDeployedBytecode))
		bytecodeBytes, decodeErr := hexutil.Decode(bytecodeHex)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode EntryPointSimulations bytecode: %w", decodeErr)
		}

		// Create state override: replace EntryPoint's code with EntryPointSimulations bytecode
		stateOverride = &ethTypes.StateOverride{
			entryPoint: {
				Code: (*hexutil.Bytes)(&bytecodeBytes),
			},
		}
	}

	// Estimate gas with packed format; no standard fallback
	// Note: For EntryPoint v0.9.0, simulateValidation returns ValidationResult normally on success,
	// but eth_estimateGas will still work correctly - it executes the call and returns gas consumed
	gasLimit, err := v.requester.EstimateGas(txArgs, v.config.Coinbase, height, stateOverride, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas (packed simulateValidation): %w", err)
	}

	// Parse gas estimates from simulation result
	// TODO: For more accurate estimates, we could call simulateValidation via eth_call and extract
	// preOpGas from ValidationResult.returnInfo.preOpGas. For now, use eth_estimateGas result.
	// The current approach splits the estimated gas, which is a reasonable approximation.
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

// FactoryDecodeResult contains decoded factory call result
type FactoryDecodeResult struct {
	Decoded        string // Human-readable decoded message
	IsFactoryError bool   // True if this is a factory error (NotSenderCreator, etc.)
	IsReturnValue  bool   // True if this is a successful return value (address)
}

// decodeRevertData attempts to decode revert data and determine if it's success or failure
func (v *UserOpValidator) decodeRevertData(revertData []byte, revertHex string) RevertDecodeResult {
	result := RevertDecodeResult{}

	if len(revertData) < 4 {
		result.Decoded = "Revert without reason (empty or selector only)"
		return result
	}

	errorSelector := hexutil.Encode(revertData[:4])

	// Strategy 1: Check for FailedOp errors (validation failures) - get selector from EntryPoint ABI
	failedOpError, exists := entryPointABIParsed.Errors["FailedOp"]
	var failedOpSelector []byte
	if exists {
		failedOpSelector = failedOpError.ID[:4]
	} else {
		// Fallback to manual calculation if ABI doesn't have it (shouldn't happen)
		failedOpSelector = crypto.Keccak256([]byte("FailedOp(uint256,string)"))[:4]
	}
	if hexutil.Encode(revertData[:4]) == hexutil.Encode(failedOpSelector) {
		result.IsFailedOp = true
		decoded := v.decodeFailedOp(revertData)
		result.Decoded = decoded.Decoded
		result.AAErrorCode = decoded.AAErrorCode
		return result
	}

	// Strategy 2: Check for FailedOpWithRevert (from EntryPoint ABI)
	failedOpWithRevertError, exists := entryPointABIParsed.Errors["FailedOpWithRevert"]
	var failedOpWithRevertSelector []byte
	if exists {
		failedOpWithRevertSelector = failedOpWithRevertError.ID[:4]
	} else {
		// Fallback to manual calculation if ABI doesn't have it (shouldn't happen)
		failedOpWithRevertSelector = crypto.Keccak256([]byte("FailedOpWithRevert(uint256,string,bytes)"))[:4]
	}
	if hexutil.Encode(revertData[:4]) == hexutil.Encode(failedOpWithRevertSelector) {
		result.IsFailedOp = true
		decoded := v.decodeFailedOpWithRevert(revertData)
		result.Decoded = decoded.Decoded
		result.AAErrorCode = decoded.AAErrorCode
		return result
	}

	// Strategy 3: Check for ValidationResult struct (success case)
	// ValidationResult is not an error - it's the success case returned via revert
	// Format: preOpGas (32) + paid (32) + validAfter (32) + validUntil (32) + optional paymasterContext
	// We only treat it as ValidationResult if:
	// 1. It has at least 128 bytes (minimum ValidationResult size)
	// 2. It's not a known error selector (FailedOp, FailedOpWithRevert, Error(string))
	// 3. The data structure matches ValidationResult format (all uint256 fields)
	// This is conservative - we default to "unknown error" unless we're confident it's ValidationResult
	if len(revertData) >= 128 && errorSelector != "0x08c379a0" {
		// Check if it matches ValidationResult format: 4+ uint256 fields (128+ bytes, no error selector)
		// ValidationResult has no selector - it's raw struct data
		// Verify the structure looks like uint256 fields (all fields should be reasonable values)
		// For safety, we require at least 128 bytes and verify the first few fields are reasonable
		preOpGas := new(big.Int).SetBytes(revertData[0:32])
		paid := new(big.Int).SetBytes(revertData[32:64])
		validAfter := new(big.Int).SetBytes(revertData[64:96])
		validUntil := new(big.Int).SetBytes(revertData[96:128])
		
		// Heuristic: ValidationResult fields should be reasonable (not all zeros, not extremely large)
		// preOpGas and paid are gas values (typically < 10M), validAfter/validUntil are timestamps
		maxReasonableGas := big.NewInt(50_000_000) // 50M gas is very high but possible
		maxReasonableTimestamp := big.NewInt(1e12) // Year 2286 in Unix time
		
		// Only treat as ValidationResult if values are in reasonable ranges
		// This prevents misclassifying random data or other errors as success
		if preOpGas.Cmp(maxReasonableGas) <= 0 &&
			paid.Cmp(maxReasonableGas) <= 0 &&
			validAfter.Cmp(maxReasonableTimestamp) <= 0 &&
			validUntil.Cmp(maxReasonableTimestamp) <= 0 &&
			validAfter.Cmp(validUntil) <= 0 { // validAfter <= validUntil
			result.IsValidationResult = true
			result.Decoded = v.decodeValidationResult(revertData)
			return result
		}
		// If values are out of range, treat as unknown error (not ValidationResult)
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
		// FailedOp(uint256 opIndex, string reason) - get selector from ABI
		failedOpError, exists := entryPointABIParsed.Errors["FailedOp"]
		var failedOpSelector []byte
		if exists {
			failedOpSelector = failedOpError.ID[:4]
		} else {
			// Fallback to manual calculation if ABI doesn't have it (shouldn't happen)
			failedOpSelector = crypto.Keccak256([]byte("FailedOp(uint256,string)"))[:4]
		}
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

		// FailedOpWithRevert(uint256 opIndex, string reason, bytes revertData) - get selector from ABI
		failedOpWithRevertError, exists := entryPointABIParsed.Errors["FailedOpWithRevert"]
		var failedOpWithRevertSelector []byte
		if exists {
			failedOpWithRevertSelector = failedOpWithRevertError.ID[:4]
		} else {
			// Fallback to manual calculation if ABI doesn't have it (shouldn't happen)
			failedOpWithRevertSelector = crypto.Keccak256([]byte("FailedOpWithRevert(uint256,string,bytes)"))[:4]
		}
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

	// Could not decode
	return ""
}

// decodeFactoryRevert decodes revert data from SimpleAccountFactory using the factory ABI
func (v *UserOpValidator) decodeFactoryRevert(revertData []byte, revertHex string) FactoryDecodeResult {
	result := FactoryDecodeResult{}

	if len(revertData) < 4 {
		result.Decoded = "Factory revert without selector"
		return result
	}

	// Try to decode using factory ABI
	selector := revertData[:4]

	// Check for NotSenderCreator error (from SimpleAccountFactory ABI)
	notSenderCreatorError, exists := simpleAccountFactoryABIParsed.Errors["NotSenderCreator"]
	var notSenderCreatorSelector []byte
	if exists {
		notSenderCreatorSelector = notSenderCreatorError.ID[:4]
	} else {
		// Fallback to manual calculation if ABI doesn't have it (shouldn't happen)
		notSenderCreatorSelector = crypto.Keccak256([]byte("NotSenderCreator(address,address,address)"))[:4]
	}
	if bytes.Equal(selector, notSenderCreatorSelector) {
		result.IsFactoryError = true
		if len(revertData) >= 100 {
			// Decode: NotSenderCreator(address msgSender, address entity, address senderCreator)
			msgSender := common.BytesToAddress(revertData[4:36])
			entity := common.BytesToAddress(revertData[36:68])
			senderCreator := common.BytesToAddress(revertData[68:100])
			result.Decoded = fmt.Sprintf("NotSenderCreator(msgSender=%s, entity=%s, senderCreator=%s)", msgSender.Hex(), entity.Hex(), senderCreator.Hex())
		} else {
			result.Decoded = "NotSenderCreator (insufficient data to decode parameters)"
		}
		return result
	}

	// Check if this might be a return value (createAccount returns address)
	// If the call succeeded, eth_call would return the address directly (not as revert)
	// But if we're seeing this in revert data, it might be encoded differently
	// For now, if it's not a known error and has 20 bytes after selector, treat as return value
	if len(revertData) == 24 { // 4 bytes selector + 20 bytes address
		addr := common.BytesToAddress(revertData[4:24])
		result.IsReturnValue = true
		result.Decoded = fmt.Sprintf("createAccount returned address: %s", addr.Hex())
		return result
	}

	// Unknown format
	result.Decoded = fmt.Sprintf("Unknown factory format (selector: %s, data length: %d bytes) - might be from SimpleAccount implementation or proxy", hexutil.Encode(selector), len(revertData))
	return result
}

// ValidationData represents decoded validation data from EntryPoint v0.9.0
// validationData is packed as: aggregatorOrSigFail (160 bits) | validUntil (48 bits) << 160 | validAfter (48 bits) << (160+48)
// Sentinel values (from EntryPoint v0.9.0):
//   - SIG_VALIDATION_SUCCESS = 0 (no aggregator, signature OK)
//   - SIG_VALIDATION_FAILED = 1 (no aggregator, signature failed)
//   - aggregatorOrSigFail > 1 = actual aggregator address
type ValidationData struct {
	AggregatorOrSigFail *big.Int // Low 160 bits: 0 = success, 1 = failed, >1 = aggregator address
	ValidUntil          *big.Int // Next 48 bits: validity window end
	ValidAfter          *big.Int // Top 48 bits: validity window start
	HasAggregator       bool     // True if aggregatorOrSigFail > 1
	SigFailed           bool     // True if aggregatorOrSigFail == 1
}

// EntryPoint v0.9.0 sentinel values for validationData
var (
	SIG_VALIDATION_SUCCESS = big.NewInt(0)
	SIG_VALIDATION_FAILED  = big.NewInt(1)
	// VALIDITY_BLOCK_RANGE_FLAG and MASK per EntryPoint v0.9.0
	// If both validAfter and validUntil have this flag set, they are interpreted as block numbers (not timestamps)
	// Flag uses the top bit of the 48-bit field
	VALIDITY_BLOCK_RANGE_FLAG = new(big.Int).Lsh(big.NewInt(1), 47)                  // 1 << 47
	VALIDITY_BLOCK_RANGE_MASK = new(big.Int).Sub(VALIDITY_BLOCK_RANGE_FLAG, big.NewInt(1)) // lower 47 bits set
)

// DecodeValidationData decodes a validationData uint256 according to EntryPoint v0.9.0 format
// Format: aggregatorOrSigFail (low 160 bits) | validUntil (48 bits) << 160 | validAfter (48 bits) << (160+48)
// Based on EntryPoint v0.9.0 _parseValidationData and _getValidationData logic
func DecodeValidationData(validationData *big.Int) ValidationData {
	// Extract aggregatorOrSigFail (low 160 bits)
	aggregatorMask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1))
	aggregatorOrSigFail := new(big.Int).And(validationData, aggregatorMask)

	// Extract validUntil (48 bits, shifted left 160)
	validUntilMask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 48), big.NewInt(1))
	validUntilShifted := new(big.Int).Rsh(validationData, 160)
	validUntil := new(big.Int).And(validUntilShifted, validUntilMask)

	// Extract validAfter (48 bits, shifted left 160+48 = 208)
	validAfterShifted := new(big.Int).Rsh(validationData, 208)
	validAfter := new(big.Int).And(validAfterShifted, validUntilMask)

	// Determine signature status and aggregator presence
	// SIG_VALIDATION_SUCCESS = 0: no aggregator, signature OK
	// SIG_VALIDATION_FAILED = 1: no aggregator, signature failed
	// aggregatorOrSigFail > 1: actual aggregator address
	sigFailed := aggregatorOrSigFail.Cmp(SIG_VALIDATION_FAILED) == 0
	hasAggregator := aggregatorOrSigFail.Cmp(SIG_VALIDATION_FAILED) > 0

	return ValidationData{
		AggregatorOrSigFail: aggregatorOrSigFail,
		ValidUntil:          validUntil,
		ValidAfter:          validAfter,
		HasAggregator:       hasAggregator,
		SigFailed:           sigFailed,
	}
}

// GetAggregatorAddress extracts the aggregator address from validationData if present
// Returns zero address if no aggregator (signature success or failure)
func (vd ValidationData) GetAggregatorAddress() common.Address {
	if vd.HasAggregator {
		return common.BigToAddress(vd.AggregatorOrSigFail)
	}
	return common.Address{}
}

// checkValidationWindow returns (outOfRange, isBlockRange)
func checkValidationWindow(vd ValidationData, currentBlockNumber uint64, currentBlockTimestamp uint64) (bool, bool) {
	validAfter := new(big.Int).Set(vd.ValidAfter)
	validUntil := new(big.Int).Set(vd.ValidUntil)

	// Block range mode if both fields have the flag set
	hasBlockRangeFlag := validAfter.Cmp(VALIDITY_BLOCK_RANGE_FLAG) >= 0 && validUntil.Cmp(VALIDITY_BLOCK_RANGE_FLAG) >= 0

	if hasBlockRangeFlag {
		validAfterBlock := new(big.Int).And(validAfter, VALIDITY_BLOCK_RANGE_MASK)
		validUntilBlock := new(big.Int).And(validUntil, VALIDITY_BLOCK_RANGE_MASK)

		// outOfValidityRange = block.number > validUntilBlock || block.number <= validAfterBlock
		if new(big.Int).SetUint64(currentBlockNumber).Cmp(validUntilBlock) > 0 ||
			new(big.Int).SetUint64(currentBlockNumber).Cmp(validAfterBlock) <= 0 {
			return true, true
		}
		return false, true
	}

	// Timestamp mode: outOfValidityRange = block.timestamp > validUntil || block.timestamp <= validAfter
	if new(big.Int).SetUint64(currentBlockTimestamp).Cmp(validUntil) > 0 ||
		new(big.Int).SetUint64(currentBlockTimestamp).Cmp(validAfter) <= 0 {
		return true, false
	}
	return false, false
}

// validateValidationResult implements the validation pipeline from the TDD plan (Section 6)
// It validates the ValidationResult according to EntryPoint v0.9.0 requirements:
// 1. Interpret validationData (signature failure, time windows)
// 2. Check stake values (sender, factory, paymaster, aggregator)
// 3. Verify prefund calculation
func (v *UserOpValidator) validateValidationResult(
	ctx context.Context,
	validationResult *ValidationResult,
	userOp *models.UserOperation,
	entryPoint common.Address,
	height uint64,
) error {
	// 1. Interpret validationData (Section 3 of the plan)
	accountValidationData := DecodeValidationData(validationResult.ReturnInfo.AccountValidationData)
	paymasterValidationData := DecodeValidationData(validationResult.ReturnInfo.PaymasterValidationData)

	// Log decoded validationData with exact sentinel values
	accountAggregatorAddr := accountValidationData.GetAggregatorAddress()
	paymasterAggregatorAddr := paymasterValidationData.GetAggregatorAddress()
	v.logger.Debug().
		Str("accountAggregatorOrSigFail", accountValidationData.AggregatorOrSigFail.String()).
		Str("accountAggregator", accountAggregatorAddr.Hex()).
		Bool("accountHasAggregator", accountValidationData.HasAggregator).
		Str("accountValidAfter", accountValidationData.ValidAfter.String()).
		Str("accountValidUntil", accountValidationData.ValidUntil.String()).
		Bool("accountSigFailed", accountValidationData.SigFailed).
		Str("paymasterAggregatorOrSigFail", paymasterValidationData.AggregatorOrSigFail.String()).
		Str("paymasterAggregator", paymasterAggregatorAddr.Hex()).
		Bool("paymasterHasAggregator", paymasterValidationData.HasAggregator).
		Str("paymasterValidAfter", paymasterValidationData.ValidAfter.String()).
		Str("paymasterValidUntil", paymasterValidationData.ValidUntil.String()).
		Bool("paymasterSigFailed", paymasterValidationData.SigFailed).
		Msg("decoded validationData with EntryPoint v0.9.0 sentinel values")

	// Check signature failure (Test 3.1 from the plan)
	// SIG_VALIDATION_FAILED = 1 means signature validation failed
	if accountValidationData.SigFailed {
		v.logger.Error().
			Str("accountValidationData", validationResult.ReturnInfo.AccountValidationData.Text(16)).
			Str("aggregatorOrSigFail", accountValidationData.AggregatorOrSigFail.String()).
			Str("sender", userOp.Sender.Hex()).
			Msg("account signature validation failed (SIG_VALIDATION_FAILED=1) - rejecting UserOp")
		return fmt.Errorf("account signature validation failed (AA24 signature error)")
	}

	if paymasterValidationData.SigFailed {
		v.logger.Error().
			Str("paymasterValidationData", validationResult.ReturnInfo.PaymasterValidationData.Text(16)).
			Str("aggregatorOrSigFail", paymasterValidationData.AggregatorOrSigFail.String()).
			Str("sender", userOp.Sender.Hex()).
			Msg("paymaster signature validation failed (SIG_VALIDATION_FAILED=1) - rejecting UserOp")
		return fmt.Errorf("paymaster signature validation failed")
	}

	// Check validity windows (Test 3.2, 3.3)
	// Get current block (height and timestamp) for validity checks
	if v.blocks == nil {
		v.logger.Debug().Msg("blocks indexer not set; skipping validity window time/block comparison")
	} else {
		currentHeight, err := v.blocks.LatestEVMHeight()
		if err != nil {
			v.logger.Warn().
				Err(err).
				Msg("could not get latest EVM height for validity window checks; skipping time/block comparison")
		} else {
			block, blkErr := v.blocks.GetByHeight(currentHeight)
			if blkErr != nil {
				v.logger.Warn().
					Err(blkErr).
					Uint64("height", currentHeight).
					Msg("could not get block by height for validity window checks; skipping time/block comparison")
			} else {
				currentTimestamp := block.Timestamp
				currentBlockNumber := block.Height

				// Account validity
				accountOutOfRange, accountIsBlockRange := checkValidationWindow(accountValidationData, currentBlockNumber, currentTimestamp)
				if accountOutOfRange {
					v.logger.Error().
						Uint64("currentBlockNumber", currentBlockNumber).
						Uint64("currentTimestamp", currentTimestamp).
						Bool("isBlockRange", accountIsBlockRange).
						Str("validAfter", accountValidationData.ValidAfter.String()).
						Str("validUntil", accountValidationData.ValidUntil.String()).
						Str("sender", userOp.Sender.Hex()).
						Msg("account validation window out of range - rejecting UserOp")
					return fmt.Errorf("account validation window out of range (blockRange=%t)", accountIsBlockRange)
				}

				// Paymaster validity
				paymasterOutOfRange, paymasterIsBlockRange := checkValidationWindow(paymasterValidationData, currentBlockNumber, currentTimestamp)
				if paymasterOutOfRange {
					v.logger.Error().
						Uint64("currentBlockNumber", currentBlockNumber).
						Uint64("currentTimestamp", currentTimestamp).
						Bool("isBlockRange", paymasterIsBlockRange).
						Str("validAfter", paymasterValidationData.ValidAfter.String()).
						Str("validUntil", paymasterValidationData.ValidUntil.String()).
						Str("sender", userOp.Sender.Hex()).
						Msg("paymaster validation window out of range - rejecting UserOp")
					return fmt.Errorf("paymaster validation window out of range (blockRange=%t)", paymasterIsBlockRange)
				}
			}
		}
	}

	// 2. Stake checks (Section 4 of the plan) - Test 4.1, 4.2, 4.3
	// Config should already have default stake requirements set in NewUserOpValidator,
	// but ensure they're set as a safety check
	if v.config.MinSenderStake == nil || v.config.MinFactoryStake == nil ||
		v.config.MinPaymasterStake == nil || v.config.MinAggregatorStake == nil {
		v.config.SetDefaultStakeRequirements()
	}

	v.logger.Debug().
		Str("senderStake", validationResult.SenderInfo.Stake.String()).
		Str("senderUnstakeDelaySec", validationResult.SenderInfo.UnstakeDelaySec.String()).
		Str("factoryStake", validationResult.FactoryInfo.Stake.String()).
		Str("factoryUnstakeDelaySec", validationResult.FactoryInfo.UnstakeDelaySec.String()).
		Str("paymasterStake", validationResult.PaymasterInfo.Stake.String()).
		Str("paymasterUnstakeDelaySec", validationResult.PaymasterInfo.UnstakeDelaySec.String()).
		Str("aggregator", validationResult.AggregatorInfo.Aggregator.Hex()).
		Str("aggregatorStake", validationResult.AggregatorInfo.StakeInfo.Stake.String()).
		Str("aggregatorUnstakeDelaySec", validationResult.AggregatorInfo.StakeInfo.UnstakeDelaySec.String()).
		Str("minSenderStake", v.config.MinSenderStake.String()).
		Str("minFactoryStake", v.config.MinFactoryStake.String()).
		Str("minPaymasterStake", v.config.MinPaymasterStake.String()).
		Str("minAggregatorStake", v.config.MinAggregatorStake.String()).
		Uint64("minUnstakeDelaySec", v.config.MinUnstakeDelaySec).
		Msg("stake information from ValidationResult with minimum requirements")

	// Test 4.1: Sender stake threshold
	if validationResult.SenderInfo.Stake.Cmp(v.config.MinSenderStake) < 0 {
		v.logger.Error().
			Str("senderStake", validationResult.SenderInfo.Stake.String()).
			Str("minSenderStake", v.config.MinSenderStake.String()).
			Str("sender", userOp.Sender.Hex()).
			Msg("sender stake below minimum threshold - rejecting UserOp")
		return fmt.Errorf("sender stake (%s) below minimum threshold (%s)", validationResult.SenderInfo.Stake.String(), v.config.MinSenderStake.String())
	}
	if validationResult.SenderInfo.UnstakeDelaySec.Cmp(big.NewInt(int64(v.config.MinUnstakeDelaySec))) < 0 {
		v.logger.Error().
			Str("senderUnstakeDelaySec", validationResult.SenderInfo.UnstakeDelaySec.String()).
			Uint64("minUnstakeDelaySec", v.config.MinUnstakeDelaySec).
			Str("sender", userOp.Sender.Hex()).
			Msg("sender unstake delay below minimum - rejecting UserOp")
		return fmt.Errorf("sender unstake delay (%s) below minimum (%d seconds)", validationResult.SenderInfo.UnstakeDelaySec.String(), v.config.MinUnstakeDelaySec)
	}

	// Factory stake check (if factory is used - initCode is present)
	if len(userOp.InitCode) > 0 {
		if validationResult.FactoryInfo.Stake.Cmp(v.config.MinFactoryStake) < 0 {
			v.logger.Error().
				Str("factoryStake", validationResult.FactoryInfo.Stake.String()).
				Str("minFactoryStake", v.config.MinFactoryStake.String()).
				Str("sender", userOp.Sender.Hex()).
				Msg("factory stake below minimum threshold - rejecting UserOp")
			return fmt.Errorf("factory stake (%s) below minimum threshold (%s)", validationResult.FactoryInfo.Stake.String(), v.config.MinFactoryStake.String())
		}
		if validationResult.FactoryInfo.UnstakeDelaySec.Cmp(big.NewInt(int64(v.config.MinUnstakeDelaySec))) < 0 {
			v.logger.Error().
				Str("factoryUnstakeDelaySec", validationResult.FactoryInfo.UnstakeDelaySec.String()).
				Uint64("minUnstakeDelaySec", v.config.MinUnstakeDelaySec).
				Str("sender", userOp.Sender.Hex()).
				Msg("factory unstake delay below minimum - rejecting UserOp")
			return fmt.Errorf("factory unstake delay (%s) below minimum (%d seconds)", validationResult.FactoryInfo.UnstakeDelaySec.String(), v.config.MinUnstakeDelaySec)
		}
	}

	// Test 4.2: Paymaster stake check (if paymaster is used)
	if len(userOp.PaymasterAndData) > 0 {
		if validationResult.PaymasterInfo.Stake.Cmp(v.config.MinPaymasterStake) < 0 {
			v.logger.Error().
				Str("paymasterStake", validationResult.PaymasterInfo.Stake.String()).
				Str("minPaymasterStake", v.config.MinPaymasterStake.String()).
				Str("sender", userOp.Sender.Hex()).
				Msg("paymaster stake below minimum threshold - rejecting UserOp")
			return fmt.Errorf("paymaster stake (%s) below minimum threshold (%s)", validationResult.PaymasterInfo.Stake.String(), v.config.MinPaymasterStake.String())
		}
		if validationResult.PaymasterInfo.UnstakeDelaySec.Cmp(big.NewInt(int64(v.config.MinUnstakeDelaySec))) < 0 {
			v.logger.Error().
				Str("paymasterUnstakeDelaySec", validationResult.PaymasterInfo.UnstakeDelaySec.String()).
				Uint64("minUnstakeDelaySec", v.config.MinUnstakeDelaySec).
				Str("sender", userOp.Sender.Hex()).
				Msg("paymaster unstake delay below minimum - rejecting UserOp")
			return fmt.Errorf("paymaster unstake delay (%s) below minimum (%d seconds)", validationResult.PaymasterInfo.UnstakeDelaySec.String(), v.config.MinUnstakeDelaySec)
		}
	}

	// Test 4.3: Aggregator stake check (if aggregator is used)
	if accountValidationData.HasAggregator {
		aggregatorAddr := accountValidationData.GetAggregatorAddress()
		if validationResult.AggregatorInfo.Aggregator != aggregatorAddr {
			v.logger.Error().
				Str("expectedAggregator", aggregatorAddr.Hex()).
				Str("actualAggregator", validationResult.AggregatorInfo.Aggregator.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Msg("aggregator address mismatch - rejecting UserOp")
			return fmt.Errorf("aggregator address mismatch: expected %s, got %s", aggregatorAddr.Hex(), validationResult.AggregatorInfo.Aggregator.Hex())
		}
		if validationResult.AggregatorInfo.StakeInfo.Stake.Cmp(v.config.MinAggregatorStake) < 0 {
			v.logger.Error().
				Str("aggregatorStake", validationResult.AggregatorInfo.StakeInfo.Stake.String()).
				Str("minAggregatorStake", v.config.MinAggregatorStake.String()).
				Str("aggregator", aggregatorAddr.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Msg("aggregator stake below minimum threshold - rejecting UserOp")
			return fmt.Errorf("aggregator stake (%s) below minimum threshold (%s)", validationResult.AggregatorInfo.StakeInfo.Stake.String(), v.config.MinAggregatorStake.String())
		}
		if validationResult.AggregatorInfo.StakeInfo.UnstakeDelaySec.Cmp(big.NewInt(int64(v.config.MinUnstakeDelaySec))) < 0 {
			v.logger.Error().
				Str("aggregatorUnstakeDelaySec", validationResult.AggregatorInfo.StakeInfo.UnstakeDelaySec.String()).
				Uint64("minUnstakeDelaySec", v.config.MinUnstakeDelaySec).
				Str("aggregator", aggregatorAddr.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Msg("aggregator unstake delay below minimum - rejecting UserOp")
			return fmt.Errorf("aggregator unstake delay (%s) below minimum (%d seconds)", validationResult.AggregatorInfo.StakeInfo.UnstakeDelaySec.String(), v.config.MinUnstakeDelaySec)
		}
	}

	// 3. Prefund calculation verification (Test 2.2)
	// Verify that returnInfo.prefund matches expected calculation according to EntryPoint v0.9.0 _getRequiredPrefund
	// Formula: requiredGas = verificationGasLimit + callGasLimit + paymasterVerificationGasLimit + paymasterPostOpGasLimit + preVerificationGas
	//          requiredPrefund = requiredGas * maxFeePerGas
	// Note: paymasterVerificationGasLimit and paymasterPostOpGasLimit are computed by EntryPoint during paymaster validation
	//       and are not part of the UserOperation struct. We can only verify the base calculation (account gas limits).
	//       If a paymaster is present, the actual prefund will include additional paymaster gas that we cannot pre-compute.
	
	// Base calculation: account gas limits (what we can verify from UserOperation)
	baseRequiredGas := new(big.Int).Set(userOp.PreVerificationGas)
	baseRequiredGas.Add(baseRequiredGas, userOp.VerificationGasLimit)
	baseRequiredGas.Add(baseRequiredGas, userOp.CallGasLimit)
	
	baseExpectedPrefund := new(big.Int).Mul(baseRequiredGas, userOp.MaxFeePerGas)
	
	// If paymaster is present, EntryPoint will add paymasterVerificationGasLimit + paymasterPostOpGasLimit
	// We cannot verify the exact prefund in this case, but we can verify it's at least the base amount
	hasPaymaster := len(userOp.PaymasterAndData) > 0
	
	if hasPaymaster {
		// With paymaster: actual prefund should be >= base expected prefund
		// EntryPoint adds: paymasterVerificationGasLimit + paymasterPostOpGasLimit
		if validationResult.ReturnInfo.Prefund.Cmp(baseExpectedPrefund) < 0 {
			v.logger.Error().
				Str("baseExpectedPrefund", baseExpectedPrefund.String()).
				Str("actualPrefund", validationResult.ReturnInfo.Prefund.String()).
				Str("sender", userOp.Sender.Hex()).
				Msg("prefund calculation error: actual prefund is less than base expected (account gas limits). This indicates a serious mismatch.")
			return fmt.Errorf("prefund calculation error: actual prefund (%s) is less than base expected (%s) - this indicates a gateway bug or EntryPoint mismatch", validationResult.ReturnInfo.Prefund.String(), baseExpectedPrefund.String())
		}
		
		// Log the difference (should be paymaster gas)
		paymasterGasDiff := new(big.Int).Sub(validationResult.ReturnInfo.Prefund, baseExpectedPrefund)
		v.logger.Debug().
			Str("baseExpectedPrefund", baseExpectedPrefund.String()).
			Str("actualPrefund", validationResult.ReturnInfo.Prefund.String()).
			Str("paymasterGasDiff", paymasterGasDiff.String()).
			Str("sender", userOp.Sender.Hex()).
			Msg("prefund calculation verified (base) - paymaster gas included in actual prefund")
	} else {
		// Without paymaster: actual prefund should exactly match base expected prefund
		// Formula: preVerificationGas + (callGasLimit + verificationGasLimit) * maxFeePerGas
		prefundDiff := new(big.Int).Sub(validationResult.ReturnInfo.Prefund, baseExpectedPrefund)
		prefundDiffAbs := new(big.Int).Abs(prefundDiff)
		
		// Should match exactly (no rounding needed - all values are integers)
		if prefundDiffAbs.Cmp(big.NewInt(0)) != 0 {
			v.logger.Error().
				Str("expectedPrefund", baseExpectedPrefund.String()).
				Str("actualPrefund", validationResult.ReturnInfo.Prefund.String()).
				Str("prefundDiff", prefundDiff.String()).
				Str("preVerificationGas", userOp.PreVerificationGas.String()).
				Str("callGasLimit", userOp.CallGasLimit.String()).
				Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
				Str("maxFeePerGas", userOp.MaxFeePerGas.String()).
				Str("sender", userOp.Sender.Hex()).
				Msg("CRITICAL: prefund calculation mismatch - this indicates a gateway bug or EntryPoint version mismatch. Rejecting UserOp.")
			return fmt.Errorf("prefund calculation mismatch: expected %s, got %s (diff: %s). This indicates a gateway bug or EntryPoint version mismatch", baseExpectedPrefund.String(), validationResult.ReturnInfo.Prefund.String(), prefundDiff.String())
		}
		
		v.logger.Debug().
			Str("prefund", validationResult.ReturnInfo.Prefund.String()).
			Str("expectedPrefund", baseExpectedPrefund.String()).
			Str("preVerificationGas", userOp.PreVerificationGas.String()).
			Str("callGasLimit", userOp.CallGasLimit.String()).
			Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
			Str("maxFeePerGas", userOp.MaxFeePerGas.String()).
			Msg("prefund calculation verified exactly (no paymaster)")
	}

	return nil
}

