package requester

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
)

// Bundler creates EntryPoint.handleOps() transactions from UserOperations
type Bundler struct {
	userOpPool UserOperationPool
	config     config.Config
	logger     zerolog.Logger
	txPool     TxPool
	requester  Requester
}

func NewBundler(
	userOpPool UserOperationPool,
	config config.Config,
	logger zerolog.Logger,
	txPool TxPool,
	requester Requester,
) *Bundler {
	return &Bundler{
		userOpPool: userOpPool,
		config:     config,
		logger:     logger.With().Str("component", "bundler").Logger(),
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
	// Track which UserOps belong to which transaction so we can remove them
	// only after the transaction is successfully submitted
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
			continue
		}

		b.logger.Info().
			Str("txHash", tx.Hash().Hex()).
			Int("batchIndex", batchIdx).
			Int("batchSize", len(batch)).
			Msg("created handleOps transaction")

		// Store transaction with its UserOps (don't remove from pool yet)
		bundledTxs = append(bundledTxs, &BundledTransaction{
			Transaction: tx,
			UserOps:     batch,
		})
	}

	return bundledTxs, nil
}

// SubmitBundledTransactions submits the bundled transactions to the transaction pool
func (b *Bundler) SubmitBundledTransactions(ctx context.Context) error {
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
		if err := b.txPool.Add(ctx, tx); err != nil {
			b.logger.Error().
				Err(err).
				Str("txHash", tx.Hash().Hex()).
				Int("txIndex", i).
				Int("totalTxs", len(bundledTxs)).
				Msg("failed to add handleOps transaction to pool")
			// Don't remove UserOps - they stay in pool for retry
			continue
		}
		successCount++

		// Remove UserOps from pool only after successful submission
		for _, userOp := range bundledTx.UserOps {
			hash, _ := userOp.Hash(b.config.EntryPointAddress, b.config.EVMNetworkID)
			b.userOpPool.Remove(hash)
			b.logger.Debug().
				Str("userOpHash", hash.Hex()).
				Str("sender", userOp.Sender.Hex()).
				Str("txHash", tx.Hash().Hex()).
				Msg("removed UserOp from pool after successful transaction submission")
		}

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

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0, // Will be set by transaction pool
		To:       &b.config.EntryPointAddress,
		Value:    big.NewInt(0),
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     calldata,
	})

	return tx, nil
}

// encodeHandleOpsCalldata encodes the calldata for EntryPoint.handleOps()
func encodeHandleOpsCalldata(userOps []*models.UserOperation, beneficiary common.Address) ([]byte, error) {
	return EncodeHandleOps(userOps, beneficiary)
}
