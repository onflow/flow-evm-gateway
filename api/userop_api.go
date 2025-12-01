package api

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester"
)

// UserOpAPI handles ERC-4337 UserOperation RPC methods
type UserOpAPI struct {
	logger      zerolog.Logger
	config      config.Config
	userOpPool  requester.UserOperationPool
	bundler     *requester.Bundler
	validator   *requester.UserOpValidator
	rateLimiter RateLimiter
	collector   metrics.Collector
}

func NewUserOpAPI(
	logger zerolog.Logger,
	config config.Config,
	userOpPool requester.UserOperationPool,
	bundler *requester.Bundler,
	validator *requester.UserOpValidator,
	rateLimiter RateLimiter,
	collector metrics.Collector,
) *UserOpAPI {
	return &UserOpAPI{
		logger:      logger.With().Str("component", "userop-api").Logger(),
		config:      config,
		userOpPool:  userOpPool,
		bundler:     bundler,
		validator:   validator,
		rateLimiter: rateLimiter,
		collector:   collector,
	}
}

// SendUserOperation accepts a UserOperation and adds it to the pool
func (u *UserOpAPI) SendUserOperation(
	ctx context.Context,
	userOpArgs models.UserOperationArgs,
	entryPoint *common.Address,
) (common.Hash, error) {
	if !u.config.BundlerEnabled {
		return common.Hash{}, fmt.Errorf("bundler is not enabled")
	}

	if u.config.IndexOnly {
		return common.Hash{}, errs.ErrIndexOnlyMode
	}

	l := u.logger.With().
		Str("endpoint", EthSendUserOperation).
		Str("sender", userOpArgs.Sender.Hex()).
		Logger()

	// Log that we received the request
	logFields := l.Info().
		Str("sender", userOpArgs.Sender.Hex())
	if userOpArgs.Nonce != nil {
		logFields = logFields.Str("nonce", userOpArgs.Nonce.String())
	}
	if userOpArgs.InitCode != nil {
		logFields = logFields.
			Int("initCodeLen", len(*userOpArgs.InitCode)).
			Str("initCodeHex", hexutil.Encode(*userOpArgs.InitCode))
		// Log factory address if initCode is long enough
		if len(*userOpArgs.InitCode) >= 24 {
			factoryAddr := common.BytesToAddress((*userOpArgs.InitCode)[0:20])
			selector := hexutil.Encode((*userOpArgs.InitCode)[20:24])
			logFields = logFields.
				Str("rawFactoryAddress", factoryAddr.Hex()).
				Str("rawFunctionSelector", selector)
		}
	}
	if userOpArgs.CallData != nil {
		logFields = logFields.Int("callDataLen", len(*userOpArgs.CallData))
	}
	logFields.Msg("received eth_sendUserOperation request")

	if err := u.rateLimiter.Apply(ctx, EthSendUserOperation); err != nil {
		l.Error().Err(err).Msg("rate limit exceeded for eth_sendUserOperation")
		return common.Hash{}, err
	}

	// Use default EntryPoint if not provided
	ep := u.config.EntryPointAddress
	if entryPoint != nil {
		ep = *entryPoint
	}

	// Convert args to UserOperation
	userOp, err := userOpArgs.ToUserOperation()
	if err != nil {
		return handleError[common.Hash](err, l, u.collector)
	}

	// Validate UserOperation (includes signature verification and simulation)
	if u.validator != nil {
		if err := u.validator.Validate(ctx, userOp, ep); err != nil {
			// Log detailed validation error for debugging
			l.Error().
				Err(err).
				Str("sender", userOp.Sender.Hex()).
				Str("nonce", userOp.Nonce.String()).
				Int("initCodeLen", len(userOp.InitCode)).
				Int("callDataLen", len(userOp.CallData)).
				Msg("user operation validation failed")
			return handleError[common.Hash](err, l, u.collector)
		}
	}

	// Add to pool
	hash, err := u.userOpPool.Add(ctx, userOp, ep)
	if err != nil {
		return handleError[common.Hash](err, l, u.collector)
	}

	// Safety check: never return a zero hash as a valid result
	// This should never happen, but if it does, return an error
	if hash == (common.Hash{}) {
		err := fmt.Errorf("internal error: user operation hash is zero")
		l.Error().Err(err).Msg("user operation hash is zero after adding to pool")
		return common.Hash{}, err
	}

	l.Info().
		Str("userOpHash", hash.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("nonce", userOp.Nonce.String()).
		Msg("user operation added to pool - will be included in next bundle")

	// Trigger bundling (async)
	go func() {
		if err := u.triggerBundling(context.Background()); err != nil {
			l.Error().
				Err(err).
				Str("userOpHash", hash.Hex()).
				Msg("failed to trigger bundling after adding UserOp to pool")
		}
	}()

	l.Info().
		Str("userOpHash", hash.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("nonce", userOp.Nonce.String()).
		Msg("user operation added to pool - will be included in next bundle")

	return hash, nil
}

// EstimateUserOperationGas estimates gas for a UserOperation
func (u *UserOpAPI) EstimateUserOperationGas(
	ctx context.Context,
	userOpArgs models.UserOperationArgs,
	entryPoint *common.Address,
) (*requester.UserOpGasEstimate, error) {
	if !u.config.BundlerEnabled {
		return nil, fmt.Errorf("bundler is not enabled")
	}

	l := u.logger.With().
		Str("endpoint", EthEstimateUserOperationGas).
		Str("sender", userOpArgs.Sender.Hex()).
		Logger()

	if err := u.rateLimiter.Apply(ctx, EthEstimateUserOperationGas); err != nil {
		return nil, err
	}

	// Use default EntryPoint if not provided
	ep := u.config.EntryPointAddress
	if entryPoint != nil {
		ep = *entryPoint
	}

	// Convert args to UserOperation
	userOp, err := userOpArgs.ToUserOperation()
	if err != nil {
		_, err2 := handleError[*requester.UserOpGasEstimate](err, l, u.collector)
		return nil, err2
	}

	// Estimate gas using validator
	if u.validator != nil {
		estimate, err := u.validator.EstimateGas(ctx, userOp, ep)
		if err != nil {
			_, err2 := handleError[*requester.UserOpGasEstimate](err, l, u.collector)
			return nil, err2
		}
		return estimate, nil
	}

	// Fallback estimates
	estimate := &requester.UserOpGasEstimate{
		PreVerificationGas: hexutil.Big(*big.NewInt(50000)),
		VerificationGas:    hexutil.Big(*big.NewInt(100000)),
		CallGasLimit:       hexutil.Big(*big.NewInt(20000)),
	}

	return estimate, nil
}

// GetUserOperationByHash retrieves a UserOperation by its hash
func (u *UserOpAPI) GetUserOperationByHash(
	ctx context.Context,
	hash common.Hash,
) (*UserOperationResult, error) {
	if !u.config.BundlerEnabled {
		return nil, fmt.Errorf("bundler is not enabled")
	}

	l := u.logger.With().
		Str("endpoint", EthGetUserOperationByHash).
		Str("hash", hash.Hex()).
		Logger()

	if err := u.rateLimiter.Apply(ctx, EthGetUserOperationByHash); err != nil {
		return nil, err
	}

	userOp, err := u.userOpPool.GetByHash(hash)
	if err != nil {
		_, err2 := handleError[*UserOperationResult](err, l, u.collector)
		return nil, err2
	}

	// TODO: Get transaction hash and block info from indexing
	result := &UserOperationResult{
		UserOperation: userOp,
		EntryPoint:    u.config.EntryPointAddress,
		BlockNumber:   nil, // TODO: Get from indexing
		BlockHash:     nil, // TODO: Get from indexing
		TransactionHash: nil, // TODO: Get from indexing
	}

	return result, nil
}

// GetUserOperationReceipt retrieves the receipt for a UserOperation
func (u *UserOpAPI) GetUserOperationReceipt(
	ctx context.Context,
	hash common.Hash,
) (*UserOperationReceipt, error) {
	if !u.config.BundlerEnabled {
		return nil, fmt.Errorf("bundler is not enabled")
	}

	l := u.logger.With().
		Str("endpoint", EthGetUserOperationReceipt).
		Str("hash", hash.Hex()).
		Logger()

	if err := u.rateLimiter.Apply(ctx, EthGetUserOperationReceipt); err != nil {
		return nil, err
	}

	// TODO: Get receipt from indexing
	// For now, return nil (not found)
	err := fmt.Errorf("user operation receipt not found")
	_, err2 := handleError[*UserOperationReceipt](err, l, u.collector)
	return nil, err2
}

// triggerBundling creates and submits bundled transactions
func (u *UserOpAPI) triggerBundling(ctx context.Context) error {
	if u.bundler == nil {
		return nil
	}

	// Submit bundled transactions (this creates and adds them to the pool)
	if err := u.bundler.SubmitBundledTransactions(ctx); err != nil {
		return fmt.Errorf("failed to submit bundled transactions: %w", err)
	}

	return nil
}


// UserOperationResult represents a UserOperation with execution status
type UserOperationResult struct {
	UserOperation    *models.UserOperation `json:"userOperation"`
	EntryPoint       common.Address        `json:"entryPoint"`
	BlockNumber      *hexutil.Big          `json:"blockNumber,omitempty"`
	BlockHash        *common.Hash          `json:"blockHash,omitempty"`
	TransactionHash  *common.Hash          `json:"transactionHash,omitempty"`
}

// UserOperationReceipt represents the receipt for a UserOperation execution
type UserOperationReceipt struct {
	UserOpHash       common.Hash   `json:"userOpHash"`
	EntryPoint       common.Address `json:"entryPoint"`
	Sender           common.Address `json:"sender"`
	Nonce            hexutil.Big   `json:"nonce"`
	Paymaster        *common.Address `json:"paymaster,omitempty"`
	ActualGasCost    hexutil.Big   `json:"actualGasCost"`
	ActualGasUsed    hexutil.Big   `json:"actualGasUsed"`
	Success          bool          `json:"success"`
	Reason           string        `json:"reason,omitempty"`
	Logs             []interface{} `json:"logs"`
	Receipt          *common.Hash  `json:"receipt,omitempty"`
}

