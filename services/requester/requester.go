package requester

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/txpool"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"

	gethParams "github.com/onflow/go-ethereum/params"
)

var (
	//go:embed cadence/run.cdc
	runTxScript []byte

	//go:embed cadence/create_coa.cdc
	createCOAScript []byte

	//go:embed cadence/get_latest_evm_height.cdc
	getLatestEVMHeight []byte
)

const minFlowBalance = 2
const coaFundingBalance = minFlowBalance - 1
const blockGasLimit = 120_000_000

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
		tx *types.LegacyTx,
		from common.Address,
		height uint64,
		stateOverrides *ethTypes.StateOverride,
	) ([]byte, error)

	// EstimateGas executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make any changes in the state/blockchain and is
	// useful to executed and retrieve the gas consumption and possible failures.
	EstimateGas(
		tx *types.LegacyTx,
		from common.Address,
		height uint64,
		stateOverrides *ethTypes.StateOverride,
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
	registerStore  *pebble.RegisterStorage
	blocksProvider *replayer.BlocksProvider
	client         *CrossSporkClient
	config         *config.Config
	signer         crypto.Signer
	txPool         *TxPool
	logger         zerolog.Logger
	blocks         storage.BlockIndexer
	mux            sync.Mutex

	head              *types.Header
	evmSigner         types.Signer
	validationOptions *txpool.ValidationOptions

	collector metrics.Collector
}

func NewEVM(
	registerStore *pebble.RegisterStorage,
	blocksProvider *replayer.BlocksProvider,
	client *CrossSporkClient,
	config *config.Config,
	signer crypto.Signer,
	logger zerolog.Logger,
	blocks storage.BlockIndexer,
	txPool *TxPool,
	collector metrics.Collector,
) (*EVM, error) {
	logger = logger.With().Str("component", "requester").Logger()
	// check that the address stores already created COA resource in the "evm" storage path.
	// if it doesn't check if the auto-creation boolean is true and if so create it
	// otherwise fail. COA resource is required by the EVM requester to be able to submit transactions.
	address := config.COAAddress
	acc, err := client.GetAccount(context.Background(), address)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch the configured COA account: %s make sure it exists: %w",
			address.String(),
			err,
		)
	}

	if acc.Balance < minFlowBalance {
		return nil, fmt.Errorf(
			"COA account must be funded with at least %d Flow, but has balance of: %d",
			minFlowBalance,
			acc.Balance,
		)
	}

	head := &types.Header{
		Number:   big.NewInt(20_182_324),
		Time:     uint64(time.Now().Unix()),
		GasLimit: blockGasLimit,
	}
	emulatorConfig := emulator.NewConfig(
		emulator.WithChainID(config.EVMNetworkID),
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
			1<<types.BlobTxType,
		MaxSize: models.TxMaxSize,
		MinTip:  new(big.Int),
	}

	evm := &EVM{
		registerStore:     registerStore,
		blocksProvider:    blocksProvider,
		client:            client,
		config:            config,
		signer:            signer,
		logger:            logger,
		blocks:            blocks,
		txPool:            txPool,
		head:              head,
		evmSigner:         evmSigner,
		validationOptions: validationOptions,
		collector:         collector,
	}

	// create COA on the account
	if config.CreateCOAResource {
		tx, err := evm.buildTransaction(
			context.Background(),
			replaceAddresses(createCOAScript, config.FlowNetworkID),
			cadence.UFix64(coaFundingBalance),
		)
		if err != nil {
			logger.Warn().Err(err).Msg("COA resource auto-creation failure")
			return nil, fmt.Errorf("COA resource auto-creation failure: %w", err)
		}
		if err := evm.client.SendTransaction(context.Background(), *tx); err != nil {
			logger.Warn().Err(err).Msg("failed to send COA resource auto-creation transaction")
			return nil, fmt.Errorf("failed to send COA resource auto-creation transaction: %w", err)
		}
	}

	return evm, nil
}

func (e *EVM) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	tx := &types.Transaction{}
	if err := tx.UnmarshalBinary(data); err != nil {
		return common.Hash{}, err
	}

	if err := models.ValidateTransaction(tx, e.head, e.evmSigner, e.validationOptions); err != nil {
		return common.Hash{}, err
	}

	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to derive the sender: %w", err)
	}

	if tx.GasPrice().Cmp(e.config.GasPrice) < 0 {
		return common.Hash{}, errs.NewTxGasPriceTooLowError(e.config.GasPrice)
	}

	txData := hex.EncodeToString(data)
	hexEncodedTx, err := cadence.NewString(txData)
	if err != nil {
		return common.Hash{}, err
	}
	coinbaseAddress, err := cadence.NewString(e.config.Coinbase.Hex())
	if err != nil {
		return common.Hash{}, err
	}

	script := replaceAddresses(runTxScript, e.config.FlowNetworkID)
	flowTx, err := e.buildTransaction(ctx, script, hexEncodedTx, coinbaseAddress)
	if err != nil {
		e.logger.Error().Err(err).Str("data", txData).Msg("failed to build transaction")
		return common.Hash{}, err
	}

	if err := e.txPool.Send(ctx, flowTx, tx); err != nil {
		return common.Hash{}, err
	}

	var to string
	if tx.To() != nil {
		to = tx.To().String()
		e.collector.EVMAccountInteraction(to)
	}

	e.logger.Info().
		Str("evm-id", tx.Hash().Hex()).
		Str("flow-id", flowTx.ID().Hex()).
		Str("to", to).
		Str("from", from.Hex()).
		Str("value", tx.Value().String()).
		Msg("raw transaction sent")

	return tx.Hash(), nil
}

// buildTransaction creates a flow transaction from the provided script with the arguments
// and signs it with the configured COA account.
func (e *EVM) buildTransaction(ctx context.Context, script []byte, args ...cadence.Value) (*flow.Transaction, error) {
	// building and signing transactions should be blocking, so we don't have keys conflict
	e.mux.Lock()
	defer e.mux.Unlock()

	var (
		g           = errgroup.Group{}
		err1, err2  error
		latestBlock *flow.Block
		index       uint32
		seqNum      uint64
	)
	// execute concurrently so we can speed up all the information we need for tx
	g.Go(func() error {
		latestBlock, err1 = e.client.GetLatestBlock(ctx, true)
		return err1
	})
	g.Go(func() error {
		index, seqNum, err2 = e.getSignerNetworkInfo(ctx)
		return err2
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	address := e.config.COAAddress
	flowTx := flow.NewTransaction().
		SetScript(script).
		SetProposalKey(address, index, seqNum).
		SetReferenceBlockID(latestBlock.ID).
		SetPayer(address)

	for _, arg := range args {
		if err := flowTx.AddArgument(arg); err != nil {
			return nil, fmt.Errorf("failed to add argument: %s, with %w", arg, err)
		}
	}

	if err := flowTx.SignEnvelope(address, index, e.signer); err != nil {
		return nil, fmt.Errorf(
			"failed to sign transaction envelope for address: %s and index: %d, with: %w",
			address,
			index,
			err)
	}

	return flowTx, nil
}

func (e *EVM) GetBalance(
	address common.Address,
	height uint64,
) (*big.Int, error) {
	view, err := e.getBlockView(height)
	if err != nil {
		return nil, err
	}

	return view.GetBalance(address)
}

func (e *EVM) GetNonce(
	address common.Address,
	height uint64,
) (uint64, error) {
	view, err := e.getBlockView(height)
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
	view, err := e.getBlockView(height)
	if err != nil {
		return common.Hash{}, err
	}

	return view.GetSlab(address, hash)
}

func (e *EVM) Call(
	tx *types.LegacyTx,
	from common.Address,
	height uint64,
	stateOverrides *ethTypes.StateOverride,
) ([]byte, error) {
	view, err := e.getBlockView(height)
	if err != nil {
		return nil, err
	}

	to := common.Address{}
	if tx.To != nil {
		to = *tx.To
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
				opts = append(opts, query.WithStateOverrideBalance(addr, (*big.Int)(*account.Balance)))
			}
			if account.State != nil && account.StateDiff != nil {
				return nil, fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
			}
			// Replace entire state if caller requires.
			if account.State != nil {
				opts = append(opts, query.WithStateOverrideState(addr, *account.State))
			}
			// Apply state diff into specified accounts.
			if account.StateDiff != nil {
				opts = append(opts, query.WithStateOverrideStateDiff(addr, *account.StateDiff))
			}
		}
	}
	result, err := view.DryCall(
		from,
		to,
		tx.Data,
		tx.Value,
		tx.Gas,
		opts...,
	)

	resultSummary := result.ResultSummary()
	if resultSummary.ErrorCode != 0 {
		if resultSummary.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return nil, errs.NewRevertError(resultSummary.ReturnedData)
		}
		return nil, errs.NewFailedTransactionError(resultSummary.ErrorMessage)
	}

	return result.ReturnedData, err
}

func (e *EVM) EstimateGas(
	tx *types.LegacyTx,
	from common.Address,
	height uint64,
	stateOverrides *ethTypes.StateOverride,
) (uint64, error) {
	view, err := e.getBlockView(height)
	if err != nil {
		return 0, err
	}

	to := common.Address{}
	if tx.To != nil {
		to = *tx.To
	}
	cdcHeight, err := e.evmToCadenceHeight(height)
	if err != nil {
		return 0, err
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
				opts = append(opts, query.WithStateOverrideBalance(addr, (*big.Int)(*account.Balance)))
			}
			if account.State != nil && account.StateDiff != nil {
				return 0, fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
			}
			// Replace entire state if caller requires.
			if account.State != nil {
				opts = append(opts, query.WithStateOverrideState(addr, *account.State))
			}
			// Apply state diff into specified accounts.
			if account.StateDiff != nil {
				opts = append(opts, query.WithStateOverrideStateDiff(addr, *account.StateDiff))
			}
		}
	}
	result, err := view.DryCall(
		from,
		to,
		tx.Data,
		tx.Value,
		tx.Gas,
		opts...,
	)
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

	if result.Successful() {
		// As mentioned in https://github.com/ethereum/EIPs/blob/master/EIPS/eip-150.md#specification
		// Define "all but one 64th" of N as N - floor(N / 64).
		// If a call asks for more gas than the maximum allowed amount
		// (i.e. the total amount of gas remaining in the parent after subtracting
		// the gas cost of the call and memory expansion), do not return an OOG error;
		// instead, if a call asks for more gas than all but one 64th of the maximum
		// allowed amount, call with all but one 64th of the maximum allowed amount of
		// gas (this is equivalent to a version of EIP-901 plus EIP-1142).
		// CREATE only provides all but one 64th of the parent gas to the child call.
		result.GasConsumed = AddOne64th(result.GasConsumed)

		// Adding `gethParams.SstoreSentryGasEIP2200` is needed for this condition:
		// https://github.com/onflow/go-ethereum/blob/master/core/vm/operations_acl.go#L29-L32
		result.GasConsumed += gethParams.SstoreSentryGasEIP2200

		// Take into account any gas refunds, which are calculated only after
		// transaction execution.
		result.GasConsumed += result.GasRefund
	}

	return result.GasConsumed, err
}

func (e *EVM) GetCode(
	address common.Address,
	height uint64,
) ([]byte, error) {
	view, err := e.getBlockView(height)
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

// getSignerNetworkInfo loads the signer account from network and returns key index and sequence number
func (e *EVM) getSignerNetworkInfo(ctx context.Context) (uint32, uint64, error) {
	account, err := e.client.GetAccount(ctx, e.config.COAAddress)
	if err != nil {
		return 0, 0, fmt.Errorf(
			"failed to get signer info account for address: %s, with: %w",
			e.config.COAAddress,
			err,
		)
	}

	e.collector.OperatorBalance(account)

	signerPub := e.signer.PublicKey()
	for _, k := range account.Keys {
		if k.PublicKey.Equals(signerPub) {
			return k.Index, k.SequenceNumber, nil
		}
	}

	return 0, 0, fmt.Errorf(
		"provided account address: %s and signer public key: %s, do not match",
		e.config.COAAddress,
		signerPub.String(),
	)
}

func (e *EVM) getBlockView(height uint64) (*query.View, error) {
	viewProvider := query.NewViewProvider(
		e.config.FlowNetworkID,
		evm.StorageAccountAddress(e.config.FlowNetworkID),
		e.registerStore,
		e.blocksProvider,
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

func AddOne64th(n uint64) uint64 {
	// NOTE: Go's integer division floors, but that is desirable here
	return n + (n / 64)
}
