package requester

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethCore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	gethParams "github.com/ethereum/go-ethereum/params"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/memorystore"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

var (
	//go:embed cadence/run.cdc
	runTxScript []byte

	//go:embed cadence/batch_run.cdc
	batchRunTxScript []byte

	//go:embed cadence/get_latest_evm_height.cdc
	getLatestEVMHeight []byte
)

const minFlowBalance = 2
const blockGasLimit = 120_000_000
const txMaxGasLimit = 50_000_000

// estimateGasErrorRatio is the amount of overestimation eth_estimateGas
// is allowed to produce in order to speed up calculations.
const estimateGasErrorRatio = 0.015

type Requester interface {
	// SendRawTransaction will submit signed transaction data to the network.
	// The submitted EVM transaction hash is returned.
	SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error)

	// GetBalance returns the amount of wei for the given address in the state of the
	// given EVM block height.
	GetBalance(address common.Address, height uint64) (*big.Int, error)

	// Call executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(
		txArgs ethTypes.TransactionArgs,
		from common.Address,
		height uint64,
		stateOverrides *ethTypes.StateOverride,
		blockOverrides *ethTypes.BlockOverrides,
	) ([]byte, error)

	// EstimateGas executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make any changes in the state/blockchain and is
	// useful to executed and retrieve the gas consumption and possible failures.
	EstimateGas(
		txArgs ethTypes.TransactionArgs,
		from common.Address,
		height uint64,
		stateOverrides *ethTypes.StateOverride,
		blockOverrides *ethTypes.BlockOverrides,
	) (uint64, error)

	// GetNonce gets nonce from the network at the given EVM block height.
	GetNonce(address common.Address, height uint64) (uint64, error)

	// GetCode returns the code stored at the given address in
	// the state for the given EVM block height.
	GetCode(address common.Address, height uint64) ([]byte, error)

	// GetStorageAt returns the storage from the state at the given address, key and block number.
	GetStorageAt(address common.Address, hash common.Hash, height uint64) (common.Hash, error)

	// GetLatestEVMHeight returns the latest EVM height of the network.
	GetLatestEVMHeight(ctx context.Context) (uint64, error)
}

var _ Requester = &EVM{}

type EVM struct {
	registerStore *pebble.RegisterStorage
	client        *CrossSporkClient
	config        config.Config
	txPool        TxPool
	logger        zerolog.Logger
	blocks        storage.BlockIndexer

	collector   metrics.Collector
	rateLimiter limiter.Store
}

func NewEVM(
	registerStore *pebble.RegisterStorage,
	client *CrossSporkClient,
	config config.Config,
	logger zerolog.Logger,
	blocks storage.BlockIndexer,
	txPool TxPool,
	collector metrics.Collector,
) (*EVM, error) {
	logger = logger.With().Str("component", "requester").Logger()

	if !config.IndexOnly {
		address := config.COAAddress
		acc, err := client.GetAccount(context.Background(), address)
		if err != nil {
			return nil, fmt.Errorf(
				"could not fetch the configured COA account: %s make sure it exists: %w",
				address.String(),
				err,
			)
		}
		// initialize the operator balance metric since it is only updated when sending a tx
		collector.OperatorBalance(acc)

		if acc.Balance < minFlowBalance {
			return nil, fmt.Errorf(
				"COA account must be funded with at least %d Flow, but has balance of: %d",
				minFlowBalance,
				acc.Balance,
			)
		}
	}

	rateLimiter, err := memorystore.New(
		&memorystore.Config{
			Tokens:   config.TxRequestLimit,
			Interval: config.TxRequestLimitDuration,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create TX rate limiter: %w", err)
	}

	return &EVM{
		registerStore: registerStore,
		client:        client,
		config:        config,
		logger:        logger,
		blocks:        blocks,
		txPool:        txPool,
		collector:     collector,
		rateLimiter:   rateLimiter,
	}, nil
}

func (e *EVM) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	tx := &types.Transaction{}
	if err := tx.UnmarshalBinary(data); err != nil {
		return common.Hash{}, err
	}

	if tx.Gas() > txMaxGasLimit {
		return common.Hash{}, errs.NewTxGasLimitTooHighError(txMaxGasLimit)
	}

	head := &types.Header{
		// `Number` is only useful to detect hard-forks which were
		// activated with block numbers. However, Ethereum now
		// activates hard-forks with timestamps, so the `Number`
		// field is not really necessary. Anyway, we set it to
		// the latest finalized block on Ethereum mainnet.
		Number: big.NewInt(22_446_370),
		// The `Time` field is what's actually used for detecting
		// whether we're in a certain hard-fork, and what kind of
		// tx validations to run.
		Time:       uint64(time.Now().Unix()),
		GasLimit:   blockGasLimit,
		Difficulty: big.NewInt(0),
	}
	emulatorConfig := emulator.NewConfig(
		emulator.WithChainID(e.config.EVMNetworkID),
		emulator.WithBlockNumber(head.Number),
		emulator.WithBlockTime(head.Time),
	)
	evmSigner := emulator.GetSigner(emulatorConfig)
	validationOptions := &txpool.ValidationOptions{
		Config: emulatorConfig.ChainConfig,
		Accept: 0 |
			1<<types.LegacyTxType |
			1<<types.AccessListTxType |
			1<<types.DynamicFeeTxType |
			1<<types.BlobTxType |
			1<<types.SetCodeTxType,
		MaxSize: models.TxMaxSize,
		MinTip:  new(big.Int),
	}
	if err := models.ValidateTransaction(tx, head, evmSigner, validationOptions); err != nil {
		return common.Hash{}, err
	}

	from, err := models.DeriveTxSender(tx)
	if err != nil {
		return common.Hash{}, err
	}

	if e.config.TxRequestLimit > 0 {
		_, _, _, ok, err := e.rateLimiter.Take(ctx, from.Hex())
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to check rate limit: %w", err)
		}
		if !ok {
			e.collector.TransactionRateLimited()
			return common.Hash{}, errs.ErrRateLimit
		}
	}

	if tx.GasPrice().Cmp(e.config.GasPrice) < 0 && e.config.EnforceGasPrice {
		return common.Hash{}, errs.NewTxGasPriceTooLowError(e.config.GasPrice)
	}

	if e.config.TxStateValidation == config.LocalIndexValidation {
		if err := e.validateTransactionWithState(tx, from); err != nil {
			return common.Hash{}, err
		}
	}

	if err := e.txPool.Add(ctx, tx); err != nil {
		return common.Hash{}, err
	}

	var to string
	if tx.To() != nil {
		to = tx.To().String()
		e.collector.EVMAccountInteraction(to)
	}

	e.logger.Info().
		Str("evm-id", tx.Hash().Hex()).
		Str("to", to).
		Str("from", from.Hex()).
		Str("value", tx.Value().String()).
		Msg("raw transaction submitted")

	return tx.Hash(), nil
}

func (e *EVM) GetBalance(
	address common.Address,
	height uint64,
) (*big.Int, error) {
	view, err := e.getBlockView(height, nil)
	if err != nil {
		return nil, err
	}

	return view.GetBalance(address)
}

func (e *EVM) GetNonce(
	address common.Address,
	height uint64,
) (uint64, error) {
	view, err := e.getBlockView(height, nil)
	if err != nil {
		return 0, err
	}

	return view.GetNonce(address)
}

func (e *EVM) GetStorageAt(
	address common.Address,
	hash common.Hash,
	height uint64,
) (common.Hash, error) {
	view, err := e.getBlockView(height, nil)
	if err != nil {
		return common.Hash{}, err
	}

	return view.GetSlab(address, hash)
}

func (e *EVM) Call(
	txArgs ethTypes.TransactionArgs,
	from common.Address,
	height uint64,
	stateOverrides *ethTypes.StateOverride,
	blockOverrides *ethTypes.BlockOverrides,
) ([]byte, error) {
	tx := txArgs.ToTransaction(types.LegacyTxType, blockGasLimit)
	result, err := e.dryRunTx(tx, from, height, stateOverrides, blockOverrides)
	if err != nil {
		return nil, err
	}

	resultSummary := result.ResultSummary()
	if resultSummary.ErrorCode != 0 {
		if resultSummary.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return nil, errs.NewRevertError(resultSummary.ReturnedData)
		}
		return nil, errs.NewFailedTransactionError(resultSummary.ErrorMessage)
	}

	return result.ReturnedData, nil
}

func (e *EVM) EstimateGas(
	txArgs ethTypes.TransactionArgs,
	from common.Address,
	height uint64,
	stateOverrides *ethTypes.StateOverride,
	blockOverrides *ethTypes.BlockOverrides,
) (uint64, error) {
	iterations := 0

	dryRun := func(gasLimit uint64) (*evmTypes.Result, error) {
		gas := hexutil.Uint64(gasLimit)
		txArgs.Gas = &gas
		tx := txArgs.ToTransaction(types.LegacyTxType, blockGasLimit)
		result, err := e.dryRunTx(tx, from, height, stateOverrides, blockOverrides)
		iterations += 1
		return result, err
	}

	// Note: The following algorithm, is largely inspired from
	// https://github.com/ethereum/go-ethereum/blob/master/eth/gasestimator/gasestimator.go#L49-L192,
	// and adapted to fit our use-case.
	// Binary search the gas limit, as it may need to be higher than the amount used
	var (
		failingGasLimit uint64 // lowest-known gas limit where tx execution fails
		passingGasLimit uint64 // lowest-known gas limit where tx execution succeeds
	)
	// Determine the highest gas limit that can be used during the estimation.
	passingGasLimit = blockGasLimit
	if txArgs.Gas != nil && (uint64(*txArgs.Gas) >= gethParams.TxGas) {
		passingGasLimit = uint64(*txArgs.Gas)
	}

	// We first execute the transaction at the highest allowable gas limit,
	// since if this fails we can return the error immediately.
	result, err := dryRun(passingGasLimit)
	if err != nil {
		return 0, err
	}
	resultSummary := result.ResultSummary()
	if resultSummary.ErrorCode != 0 {
		if resultSummary.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return 0, errs.NewRevertError(resultSummary.ReturnedData)
		}
		return 0, errs.NewFailedTransactionError(resultSummary.ErrorMessage)
	}

	// We do not want to report iterations for calls/transactions
	// that errored out or had their execution reverted.
	defer func() {
		e.collector.GasEstimationIterations(iterations)
	}()

	// For almost any transaction, the gas consumed by the unconstrained execution
	// above lower-bounds the gas limit required for it to succeed. One exception
	// is those that explicitly check gas remaining in order to execute within a
	// given limit, but we probably don't want to return the lowest possible gas
	// limit for these cases anyway.
	failingGasLimit = result.GasConsumed - 1

	// There's a fairly high chance for the transaction to execute successfully
	// with gasLimit set to the first execution's MaxGasConsumed + CallStipend.
	// Explicitly check that gas amount and use as a limit for the binary search.
	optimisticGasLimit := (result.MaxGasConsumed + gethParams.CallStipend) * 64 / 63
	if optimisticGasLimit < passingGasLimit {
		result, err := dryRun(optimisticGasLimit)
		if err != nil {
			// This should not happen under normal conditions since if we make it this far the
			// transaction had run without error at least once before.
			return 0, err
		}
		if result.Failed() {
			failingGasLimit = optimisticGasLimit
		} else {
			passingGasLimit = optimisticGasLimit
		}
	}

	// Binary search for the smallest gas limit that allows the tx to execute successfully.
	for failingGasLimit+1 < passingGasLimit {
		// It is a bit pointless to return a perfect estimation, as changing
		// network conditions require the caller to bump it up anyway. Since
		// wallets tend to use 20-25% bump, allowing a small approximation
		// error is fine (as long as it's upwards).
		if float64(passingGasLimit-failingGasLimit)/float64(passingGasLimit) < estimateGasErrorRatio {
			break
		}
		mid := min((passingGasLimit+failingGasLimit)/2,
			// Most txs don't need much higher gas limit than their gas used, and most txs don't
			// require near the full block limit of gas, so the selection of where to bisect the
			// range here is skewed to favor the low side.
			failingGasLimit*2)
		result, err := dryRun(mid)
		if err != nil {
			return 0, err
		}
		if result.Failed() {
			failingGasLimit = mid
		} else {
			passingGasLimit = mid
		}
	}

	if txArgs.AccessList != nil {
		passingGasLimit += uint64(len(*txArgs.AccessList)) * gethParams.TxAccessListAddressGas
		passingGasLimit += uint64(txArgs.AccessList.StorageKeys()) * gethParams.TxAccessListStorageKeyGas
	}
	if txArgs.AuthorizationList != nil {
		passingGasLimit += uint64(len(txArgs.AuthorizationList)) * gethParams.CallNewAccountGas
	}

	return passingGasLimit, nil
}

func (e *EVM) GetCode(
	address common.Address,
	height uint64,
) ([]byte, error) {
	view, err := e.getBlockView(height, nil)
	if err != nil {
		return nil, err
	}

	return view.GetCode(address)
}

func (e *EVM) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	val, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		replaceAddresses(getLatestEVMHeight, e.config.FlowNetworkID),
		nil,
	)
	if err != nil {
		return 0, err
	}

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt64); !ok {
		return 0, fmt.Errorf("failed to convert height %v to UInt64, got type: %T", val, val)
	}

	height := uint64(val.(cadence.UInt64))

	e.logger.Debug().
		Uint64("evm-height", height).
		Msg("get latest evm height executed")

	return height, nil
}

func (e *EVM) getBlockView(
	height uint64,
	blockOverrides *ethTypes.BlockOverrides,
) (*query.View, error) {
	blocksProvider := NewOverridableBlocksProvider(
		e.blocks,
		e.config.FlowNetworkID,
		nil,
	)

	if blockOverrides != nil {
		blocksProvider = blocksProvider.WithBlockOverrides(blockOverrides)
	}

	viewProvider := query.NewViewProvider(
		e.config.FlowNetworkID,
		evm.StorageAccountAddress(e.config.FlowNetworkID),
		e.registerStore,
		blocksProvider,
		blockGasLimit,
	)

	return viewProvider.GetBlockView(height)
}

func (e *EVM) evmToCadenceHeight(height uint64) (uint64, error) {
	cadenceHeight, err := e.blocks.GetCadenceHeight(height)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to map evm height: %d to cadence height: %w",
			height,
			err,
		)
	}

	return cadenceHeight, nil
}

func (e *EVM) dryRunTx(
	tx *types.Transaction,
	from common.Address,
	height uint64,
	stateOverrides *ethTypes.StateOverride,
	blockOverrides *ethTypes.BlockOverrides,
) (*evmTypes.Result, error) {
	view, err := e.getBlockView(height, blockOverrides)
	if err != nil {
		return nil, err
	}

	to := common.Address{}
	if tx.To() != nil {
		to = *tx.To()
	}
	cdcHeight, err := e.evmToCadenceHeight(height)
	if err != nil {
		return nil, err
	}
	rca := NewRemoteCadenceArch(cdcHeight, e.client, e.config.FlowNetworkID)
	opts := []query.DryCallOption{}
	opts = append(opts, query.WithExtraPrecompiledContracts([]evmTypes.PrecompiledContract{rca}))
	if stateOverrides != nil {
		for addr, account := range *stateOverrides {
			// Override account nonce.
			if account.Nonce != nil {
				opts = append(opts, query.WithStateOverrideNonce(addr, uint64(*account.Nonce)))
			}
			// Override account(contract) code.
			if account.Code != nil {
				opts = append(opts, query.WithStateOverrideCode(addr, *account.Code))
			}
			// Override account balance.
			if account.Balance != nil {
				opts = append(opts, query.WithStateOverrideBalance(addr, (*big.Int)(account.Balance)))
			}
			if account.State != nil && account.StateDiff != nil {
				return nil, fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
			}
			// Replace entire state if caller requires.
			if account.State != nil {
				opts = append(opts, query.WithStateOverrideState(addr, account.State))
			}
			// Apply state diff into specified accounts.
			if account.StateDiff != nil {
				opts = append(opts, query.WithStateOverrideStateDiff(addr, account.StateDiff))
			}
		}
	}
	result, err := view.DryCall(
		from,
		to,
		tx.Data(),
		tx.Value(),
		tx.Gas(),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// validateTransactionWithState checks if the given tx has the correct
// nonce & balance, according to the local state.
func (e *EVM) validateTransactionWithState(
	tx *types.Transaction,
	from common.Address,
) error {
	height, err := e.blocks.LatestEVMHeight()
	if err != nil {
		return err
	}
	view, err := e.getBlockView(height, nil)
	if err != nil {
		return err
	}

	nonce, err := view.GetNonce(from)
	if err != nil {
		return err
	}

	// Ensure the transaction adheres to nonce ordering
	if tx.Nonce() < nonce {
		return fmt.Errorf(
			"%w: address %s, tx: %v, state: %v",
			gethCore.ErrNonceTooLow,
			from,
			tx.Nonce(),
			nonce,
		)
	}

	// Ensure the transactor has enough funds to cover the transaction costs
	cost := tx.Cost()
	balance, err := view.GetBalance(from)
	if err != nil {
		return err
	}

	if balance.Cmp(cost) < 0 {
		return fmt.Errorf(
			"%w: balance %v, tx cost %v, overshot %v",
			gethCore.ErrInsufficientFunds,
			balance,
			cost,
			new(big.Int).Sub(cost, balance),
		)
	}

	return nil
}
