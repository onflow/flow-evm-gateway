package requester

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/models"
)

// Bundler creates EntryPoint.handleOps() transactions from UserOperations
type Bundler struct {
	userOpPool UserOperationPool
	config     config.Config
	logger     zerolog.Logger
	txPool     TxPool
	requester  Requester
	mu         sync.Mutex // Protects against concurrent bundler execution
}

func NewBundler(
	userOpPool UserOperationPool,
	config config.Config,
	logger zerolog.Logger,
	txPool TxPool,
	requester Requester,
) *Bundler {
	bundlerLogger := logger.With().Str("component", "bundler").Logger()
	
	// Validate EVMNetworkID is configured correctly
	// This should never happen if parseConfigFromFlags() worked correctly
	// EVMNetworkID must never be nil or zero - this indicates a bug in config parsing
	if config.EVMNetworkID == nil {
		panic("EVMNetworkID is nil - this is a bug. Config should have been validated in parseConfigFromFlags()")
	}
	if config.EVMNetworkID.Sign() == 0 {
		panic(fmt.Sprintf("EVMNetworkID is zero - this is a bug. Config should have been validated in parseConfigFromFlags(). EVMNetworkID must never be zero"))
	}
	
	bundlerLogger.Info().
		Str("evmNetworkID", config.EVMNetworkID.String()).
		Msg("bundler initialized with EVMNetworkID")
	
	return &Bundler{
		userOpPool: userOpPool,
		config:     config,
		logger:     bundlerLogger,
		txPool:     txPool,
		requester:  requester,
	}
}

// BundledTransaction represents a transaction with its associated UserOperations
type BundledTransaction struct {
	Transaction *types.Transaction
	UserOps     []*models.UserOperation
}

// CreateBundledTransactions creates multiple EntryPoint.handleOps() transactions
// that will be batched together via EVM.batchRun() in a single Cadence transaction
// Returns transactions with their associated UserOps so they can be removed from
// the pool only after successful submission
func (b *Bundler) CreateBundledTransactions(ctx context.Context) ([]*BundledTransaction, error) {
	if !b.config.BundlerEnabled {
		return nil, fmt.Errorf("bundler is not enabled")
	}

	// Get pending UserOperations
	pending := b.userOpPool.GetPending()
	b.logger.Debug().
		Int("pendingCount", len(pending)).
		Msg("checking for pending UserOperations")

	if len(pending) == 0 {
		return nil, nil
	}

	b.logger.Info().
		Int("pendingCount", len(pending)).
		Msg("found pending UserOperations - creating bundled transactions")

	// Group by sender and sort by nonce
	grouped := make(map[common.Address][]*models.UserOperation)
	for _, userOp := range pending {
		grouped[userOp.Sender] = append(grouped[userOp.Sender], userOp)
	}

	// Sort each group by nonce
	for sender := range grouped {
		sort.Slice(grouped[sender], func(i, j int) bool {
			return grouped[sender][i].Nonce.Cmp(grouped[sender][j].Nonce) < 0
		})
	}

	// Create batches respecting MaxOpsPerBundle
	var allBatches [][]*models.UserOperation
	for _, userOps := range grouped {
		for i := 0; i < len(userOps); i += b.config.MaxOpsPerBundle {
			end := i + b.config.MaxOpsPerBundle
			if end > len(userOps) {
				end = len(userOps)
			}
			allBatches = append(allBatches, userOps[i:end])
		}
	}

	// Create EntryPoint.handleOps() transactions
	// Remove UserOps from pool immediately after transaction creation to prevent duplicates
	// If submission fails, UserOps will need to be resubmitted by the user (standard ERC-4337 behavior)
	var bundledTxs []*BundledTransaction

	for batchIdx, batch := range allBatches {
		b.logger.Info().
			Int("batchIndex", batchIdx).
			Int("batchSize", len(batch)).
			Msg("creating handleOps transaction for batch")

		tx, err := b.createHandleOpsTransaction(ctx, batch)
		if err != nil {
			b.logger.Error().
				Err(err).
				Int("batchIndex", batchIdx).
				Int("batchSize", len(batch)).
				Msg("failed to create handleOps transaction")
			// Don't remove UserOps - they stay in pool for retry
			continue
		}

		b.logger.Info().
			Str("txHash", tx.Hash().Hex()).
			Int("batchIndex", batchIdx).
			Int("batchSize", len(batch)).
			Msg("created handleOps transaction")

		// Remove UserOps from pool immediately to prevent duplicate transactions
		// This prevents the bundler from creating the same transaction multiple times
		// if it runs again before the transaction is submitted
		for _, userOp := range batch {
			hash, _ := userOp.Hash(b.config.EntryPointAddress, b.config.EVMNetworkID)
			b.userOpPool.Remove(hash)
			b.logger.Info().
				Str("userOpHash", hash.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Str("txHash", tx.Hash().Hex()).
				Msg("removed UserOp from pool after transaction creation")
		}

		// Store transaction with its UserOps
		bundledTxs = append(bundledTxs, &BundledTransaction{
			Transaction: tx,
			UserOps:     batch,
		})
	}

	return bundledTxs, nil
}

// SubmitBundledTransactions submits the bundled transactions to the transaction pool
// This method is thread-safe and prevents concurrent execution
func (b *Bundler) SubmitBundledTransactions(ctx context.Context) error {
	// Acquire lock to prevent concurrent bundler execution
	// This prevents race conditions where multiple bundler ticks see the same UserOps
	b.mu.Lock()
	defer b.mu.Unlock()

	// Get pending count first for logging
	pending := b.userOpPool.GetPending()
	pendingCount := len(pending)

	// Log bundler tick at Info level for visibility
	b.logger.Info().
		Int("pendingUserOpCount", pendingCount).
		Msg("bundler tick - checking for pending UserOperations")

	bundledTxs, err := b.CreateBundledTransactions(ctx)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to create bundled transactions")
		return err
	}

	if len(bundledTxs) == 0 {
		if pendingCount > 0 {
			b.logger.Warn().
				Int("pendingUserOpCount", pendingCount).
				Msg("bundler tick found pending UserOps but created no transactions - this may indicate an issue")
		} else {
			b.logger.Debug().Msg("no pending UserOperations to bundle")
		}
		return nil
	}

	b.logger.Info().
		Int("transactionCount", len(bundledTxs)).
		Int("pendingUserOpCount", pendingCount).
		Msg("created bundled transactions - submitting to transaction pool")

	// Add each transaction to the pool
	// They will be automatically batched by the existing BatchTxPool or SingleTxPool
	// Only remove UserOps from pool after successful submission
	successCount := 0
	for i, bundledTx := range bundledTxs {
		tx := bundledTx.Transaction
		
		// Log transaction details before submission
		chainID := tx.ChainId()
		from, _ := types.Sender(types.LatestSignerForChainID(chainID), tx)
		b.logger.Info().
			Str("txHash", tx.Hash().Hex()).
			Str("from", from.Hex()).
			Str("to", tx.To().Hex()).
			Uint64("nonce", tx.Nonce()).
			Uint64("gas", tx.Gas()).
			Str("gasPrice", tx.GasPrice().String()).
			Str("chainID", chainID.String()).
			Str("expectedChainID", b.config.EVMNetworkID.String()).
			Bool("chainIDMatches", chainID.Cmp(b.config.EVMNetworkID) == 0).
			Int("txIndex", i).
			Int("totalTxs", len(bundledTxs)).
			Msg("bundler: submitting transaction to pool")
		
		if err := b.txPool.Add(ctx, tx); err != nil {
			// Extract detailed error information
			errorStr := err.Error()
			b.logger.Error().
				Err(err).
				Str("error", errorStr).
				Str("txHash", tx.Hash().Hex()).
				Str("from", from.Hex()).
				Str("chainID", chainID.String()).
				Str("expectedChainID", b.config.EVMNetworkID.String()).
				Int("txIndex", i).
				Int("totalTxs", len(bundledTxs)).
				Msg("bundler: failed to add handleOps transaction to pool")
			// Don't remove UserOps - they stay in pool for retry
			continue
		}
		
		b.logger.Info().
			Str("txHash", tx.Hash().Hex()).
			Int("txIndex", i).
			Int("totalTxs", len(bundledTxs)).
			Msg("bundler: successfully added transaction to pool")
		successCount++
		
		// Note: UserOps were already removed from pool when transaction was created
		// This prevents duplicate transactions if bundler runs again before submission

		b.logger.Info().
			Str("txHash", tx.Hash().Hex()).
			Int("txIndex", i).
			Int("totalTxs", len(bundledTxs)).
			Int("userOpCount", len(bundledTx.UserOps)).
			Str("entryPoint", b.config.EntryPointAddress.Hex()).
			Msg("submitted bundled transaction to pool - UserOps will be included in next block")
	}

	if successCount > 0 {
		b.logger.Info().
			Int("successCount", successCount).
			Int("totalTxs", len(bundledTxs)).
			Msg("bundler successfully submitted transactions to pool")
	} else {
		b.logger.Error().
			Int("totalTxs", len(bundledTxs)).
			Msg("bundler failed to submit any transactions to pool - all Add() calls failed - UserOps remain in pool for retry")
	}

	return nil
}

// createHandleOpsTransaction creates a single EntryPoint.handleOps() transaction
func (b *Bundler) createHandleOpsTransaction(ctx context.Context, userOps []*models.UserOperation) (*types.Transaction, error) {
	if len(userOps) == 0 {
		return nil, fmt.Errorf("empty user operations batch")
	}

	// Use BundlerBeneficiary or fallback to Coinbase
	beneficiary := b.config.BundlerBeneficiary
	if beneficiary == (common.Address{}) {
		beneficiary = b.config.Coinbase
	}

	// Encode UserOperations for handleOps calldata
	calldata, err := encodeHandleOpsCalldata(userOps, beneficiary)
	if err != nil {
		return nil, fmt.Errorf("failed to encode handleOps calldata: %w", err)
	}

	// Estimate gas for the handleOps call
	// Create a transaction args for estimation
	txArgs := ethTypes.TransactionArgs{
		To:   &b.config.EntryPointAddress,
		Data: (*hexutil.Bytes)(&calldata),
	}

	height, err := b.requester.GetLatestEVMHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	gasLimit, err := b.requester.EstimateGas(txArgs, b.config.Coinbase, height, nil, nil)
	if err != nil {
		// Fallback to sum of UserOp gas limits if estimation fails
		gasLimit = uint64(0)
		for _, userOp := range userOps {
			if userOp.CallGasLimit != nil {
				gasLimit += userOp.CallGasLimit.Uint64()
			}
			if userOp.VerificationGasLimit != nil {
				gasLimit += userOp.VerificationGasLimit.Uint64()
			}
			if userOp.PreVerificationGas != nil {
				gasLimit += userOp.PreVerificationGas.Uint64()
			}
		}
		gasLimit += 100000 // Add overhead
	}

	// Use gas price from config or calculate from UserOp fees
	gasPrice := b.config.GasPrice
	if gasPrice == nil || gasPrice.Sign() == 0 {
		// Calculate average maxFeePerGas from UserOps
		totalFee := big.NewInt(0)
		count := 0
		for _, userOp := range userOps {
			if userOp.MaxFeePerGas != nil {
				totalFee.Add(totalFee, userOp.MaxFeePerGas)
				count++
			}
		}
		if count > 0 {
			gasPrice = new(big.Int).Div(totalFee, big.NewInt(int64(count)))
		} else {
			gasPrice = big.NewInt(100000000) // Default 100 gwei
		}
	}

	// Get nonce for Coinbase, accounting for pending transactions
	networkNonce, err := b.requester.GetNonce(b.config.Coinbase, height)
	if err != nil {
		// If the address doesn't exist yet (entity not found), default to nonce 0
		if errors.Is(err, errs.ErrEntityNotFound) {
			networkNonce = 0
			b.logger.Debug().
				Str("coinbase", b.config.Coinbase.Hex()).
				Msg("Coinbase address not found in state, defaulting to nonce 0")
		} else {
			return nil, fmt.Errorf("failed to get nonce for Coinbase: %w", err)
		}
	}

	// Account for pending transactions in the pool
	// GetPendingNonce returns the highest nonce in the pool (or 0 if pool is empty)
	// If pendingNonce > networkNonce, there are pending transactions with higher nonces,
	// so we should use pendingNonce + 1.
	// If pendingNonce == networkNonce, we can't distinguish between:
	//   - Pool is empty (should use networkNonce)
	//   - There's a pending tx with networkNonce (should use networkNonce + 1)
	// To be safe, we use networkNonce first. If it fails, the bundler will retry with the correct nonce.
	pendingNonce := b.txPool.GetPendingNonce(b.config.Coinbase)
	nonce := networkNonce
	if pendingNonce > networkNonce {
		// Pending transactions exist with nonces > networkNonce
		// Use the next nonce after the highest pending nonce
		nonce = pendingNonce + 1
		b.logger.Info().
			Uint64("networkNonce", networkNonce).
			Uint64("pendingNonce", pendingNonce).
			Uint64("finalNonce", nonce).
			Str("coinbase", b.config.Coinbase.Hex()).
			Msg("accounted for pending transactions in bundler nonce calculation")
	} else {
		// pendingNonce <= networkNonce: use network nonce directly
		// This handles both empty pool and pendingNonce == networkNonce cases
		b.logger.Info().
			Uint64("networkNonce", networkNonce).
			Uint64("pendingNonce", pendingNonce).
			Uint64("finalNonce", nonce).
			Str("coinbase", b.config.Coinbase.Hex()).
			Msg("using network nonce (no pending transactions with higher nonces)")
	}

	// Create unsigned transaction
	// Use LegacyTx with EIP-155 signer to embed chain ID in signature
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &b.config.EntryPointAddress,
		Value:    big.NewInt(0),
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     calldata,
	})

	// Sign the transaction with Coinbase's private key
	if b.config.WalletKey == nil {
		return nil, fmt.Errorf("WalletKey not configured - cannot sign bundler transactions")
	}

	// Verify that WalletKey corresponds to Coinbase address
	expectedAddress := crypto.PubkeyToAddress(b.config.WalletKey.PublicKey)
	if expectedAddress != b.config.Coinbase {
		return nil, fmt.Errorf("WalletKey address (%s) does not match Coinbase address (%s) - bundler cannot sign transactions", expectedAddress.Hex(), b.config.Coinbase.Hex())
	}

	// Validate EVMNetworkID is configured correctly (should never fail if config was validated)
	// This is a defensive check in case the config was modified after initialization
	// EVMNetworkID must never be nil or zero - this indicates a bug
	if b.config.EVMNetworkID == nil {
		panic("EVMNetworkID is nil - this is a bug. Config should have been validated in parseConfigFromFlags() and NewBundler()")
	}
	if b.config.EVMNetworkID.Sign() == 0 {
		panic(fmt.Sprintf("EVMNetworkID is zero - this is a bug. EVMNetworkID must never be zero. Current value: %s", b.config.EVMNetworkID.String()))
	}

	// Log transaction details before signing with explicit chain ID value
	chainIDValue := b.config.EVMNetworkID.String()
	b.logger.Info().
		Str("coinbase", b.config.Coinbase.Hex()).
		Str("entryPoint", b.config.EntryPointAddress.Hex()).
		Uint64("nonce", nonce).
		Uint64("gasLimit", gasLimit).
		Str("gasPrice", gasPrice.String()).
		Int("calldataLen", len(calldata)).
		Str("evmNetworkID", chainIDValue).
		Bool("evmNetworkIDIsNil", b.config.EVMNetworkID == nil).
		Msg("bundler: creating transaction before signing")

	// Create signer with the correct chain ID from config
	// EVMNetworkID is derived from FLOW_NETWORK_ID:
	// - flow-testnet → 545
	// - flow-mainnet → 747
	// Log the actual value being passed to help debug
	b.logger.Info().
		Str("chainIDValue", chainIDValue).
		Str("chainIDPointer", fmt.Sprintf("%p", b.config.EVMNetworkID)).
		Str("chainIDString", b.config.EVMNetworkID.String()).
		Int64("chainIDInt64", b.config.EVMNetworkID.Int64()).
		Msg("bundler: creating emulator config with chain ID")
	
	// Double-check the chain ID before passing to emulator
	if b.config.EVMNetworkID == nil {
		panic("EVMNetworkID is nil when creating emulator config - this should have been caught earlier")
	}
	if b.config.EVMNetworkID.Sign() == 0 {
		panic(fmt.Sprintf("EVMNetworkID is zero when creating emulator config - this should have been caught earlier. Value: %s", b.config.EVMNetworkID.String()))
	}
	
	// Use EIP-155 signer directly to ensure chain ID is embedded in signature.
	// This is the standard practice for signing LegacyTx with chain ID (EIP-155).
	// The emulator's GetSigner() returns FrontierSigner which doesn't support chain ID.
	// Validation in models.DeriveTxSender() expects tx.ChainId() to return the chain ID,
	// which requires using an EIP-155 signer (or newer transaction types).
	signer := types.NewEIP155Signer(b.config.EVMNetworkID)
	
	// Log signer details
	b.logger.Info().
		Str("evmNetworkID", b.config.EVMNetworkID.String()).
		Str("signerType", fmt.Sprintf("%T", signer)).
		Msg("bundler: created EIP-155 signer with chain ID")

	signedTx, err := types.SignTx(tx, signer, b.config.WalletKey)
	if err != nil {
		b.logger.Error().
			Err(err).
			Str("evmNetworkID", b.config.EVMNetworkID.String()).
			Str("coinbase", b.config.Coinbase.Hex()).
			Uint64("nonce", nonce).
			Msg("bundler: failed to sign transaction")
		return nil, fmt.Errorf("failed to sign bundler transaction: %w", err)
	}

	// Log signed transaction details
	chainID := signedTx.ChainId()
	v, r, s := signedTx.RawSignatureValues()
	b.logger.Info().
		Uint64("nonce", nonce).
		Str("coinbase", b.config.Coinbase.Hex()).
		Str("txHash", signedTx.Hash().Hex()).
		Str("signedChainID", chainID.String()).
		Str("expectedChainID", b.config.EVMNetworkID.String()).
		Bool("chainIDMatches", chainID.Cmp(b.config.EVMNetworkID) == 0).
		Str("v", v.String()).
		Str("r", r.String()).
		Str("s", s.String()).
		Msg("bundler: signed transaction successfully")

	return signedTx, nil
}

// encodeHandleOpsCalldata encodes the calldata for EntryPoint.handleOps()
func encodeHandleOpsCalldata(userOps []*models.UserOperation, beneficiary common.Address) ([]byte, error) {
	return EncodeHandleOps(userOps, beneficiary)
}
