package requester

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/txpool"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"

	gethParams "github.com/onflow/go-ethereum/params"
)

var (
	//go:embed cadence/dry_run.cdc
	dryRunScript []byte

	//go:embed cadence/run.cdc
	runTxScript []byte

	//go:embed cadence/get_balance.cdc
	getBalanceScript []byte

	//go:embed cadence/create_coa.cdc
	createCOAScript []byte

	//go:embed cadence/get_nonce.cdc
	getNonceScript []byte

	//go:embed cadence/get_code.cdc
	getCodeScript []byte

	//go:embed cadence/get_latest_evm_height.cdc
	getLatestEVMHeight []byte
)

type scriptType int

const (
	dryRun scriptType = iota
	getBalance
	getNonce
	getCode
	getLatest
)

var scripts = map[scriptType][]byte{
	dryRun:     dryRunScript,
	getBalance: getBalanceScript,
	getNonce:   getNonceScript,
	getCode:    getCodeScript,
	getLatest:  getLatestEVMHeight,
}

const minFlowBalance = 2
const coaFundingBalance = minFlowBalance - 1

const LatestBlockHeight uint64 = math.MaxUint64 - 1

// TODO(janezp): Requester does need to know about special EVM block heights. evmHeight should be uint64.
type Requester interface {
	// SendRawTransaction will submit signed transaction data to the network.
	// The submitted EVM transaction hash is returned.
	SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error)

	// GetBalance returns the amount of wei for the given address in the state of the
	// given EVM block height.
	GetBalance(ctx context.Context, address common.Address, evmHeight int64) (*big.Int, error)

	// Call executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(ctx context.Context, tx *types.LegacyTx, from common.Address, evmHeight int64) ([]byte, error)

	// EstimateGas executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make any changes in the state/blockchain and is
	// useful to executed and retrieve the gas consumption and possible failures.
	EstimateGas(ctx context.Context, tx *types.LegacyTx, from common.Address, evmHeight int64) (uint64, error)

	// GetNonce gets nonce from the network at the given EVM block height.
	GetNonce(ctx context.Context, address common.Address, evmHeight int64) (uint64, error)

	// GetCode returns the code stored at the given address in
	// the state for the given EVM block height.
	GetCode(ctx context.Context, address common.Address, evmHeight int64) ([]byte, error)

	// GetLatestEVMHeight returns the latest EVM height of the network.
	GetLatestEVMHeight(ctx context.Context) (uint64, error)

	// GetStorageAt returns the storage from the state at the given address, key and block number.
	GetStorageAt(ctx context.Context, address common.Address, hash common.Hash, evmHeight int64) (common.Hash, error)
}

var _ Requester = &EVM{}

type EVM struct {
	store          *pebble.Storage
	registerStore  *pebble.RegisterStorage
	blocksProvider *replayer.BlocksProvider
	client         *CrossSporkClient
	config         *config.Config
	signer         crypto.Signer
	txPool         *TxPool
	logger         zerolog.Logger
	blocks         storage.BlockIndexer
	mux            sync.Mutex
	scriptCache    *expirable.LRU[string, cadence.Value]

	head              *types.Header
	evmSigner         types.Signer
	validationOptions *txpool.ValidationOptions

	collector metrics.Collector
}

func NewEVM(
	store *pebble.Storage,
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
		GasLimit: 30_000_000,
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

	var cache *expirable.LRU[string, cadence.Value]
	if config.CacheSize != 0 {
		cache = expirable.NewLRU[string, cadence.Value](int(config.CacheSize), nil, time.Second)
	}

	evm := &EVM{
		store:             store,
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
		scriptCache:       cache,
	}

	// create COA on the account
	if config.CreateCOAResource {
		tx, err := evm.buildTransaction(
			context.Background(),
			evm.replaceAddresses(createCOAScript),
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

	script := e.replaceAddresses(runTxScript)
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
		SetPayer(address).
		AddAuthorizer(address)

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
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) (*big.Int, error) {
	view, err := e.getBlockView(uint64(evmHeight))
	if err != nil {
		return nil, err
	}

	return view.GetBalance(address)
}

func (e *EVM) GetNonce(
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) (uint64, error) {
	view, err := e.getBlockView(uint64(evmHeight))
	if err != nil {
		return 0, err
	}

	return view.GetNonce(address)
}

func (e *EVM) GetStorageAt(
	ctx context.Context,
	address common.Address,
	hash common.Hash,
	evmHeight int64,
) (common.Hash, error) {
	view, err := e.getBlockView(uint64(evmHeight))
	if err != nil {
		return common.Hash{}, err
	}

	return view.GetSlab(address, hash)
}

func (e *EVM) Call(
	ctx context.Context,
	tx *types.LegacyTx,
	from common.Address,
	evmHeight int64,
) ([]byte, error) {
	view, err := e.getBlockView(uint64(evmHeight))
	if err != nil {
		return nil, err
	}

	to := common.Address{}
	if tx.To != nil {
		to = *tx.To
	}
	cdcHeight, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return nil, err
	}
	rca := NewRemoteCadenceArch(cdcHeight, e.client, e.config.FlowNetworkID)
	result, err := view.DryCall(
		from,
		to,
		tx.Data,
		tx.Value,
		tx.Gas,
		query.WithExtraPrecompiledContracts([]evmTypes.PrecompiledContract{rca}),
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
	ctx context.Context,
	tx *types.LegacyTx,
	from common.Address,
	evmHeight int64,
) (uint64, error) {
	view, err := e.getBlockView(uint64(evmHeight))
	if err != nil {
		return 0, err
	}

	to := common.Address{}
	if tx.To != nil {
		to = *tx.To
	}
	cdcHeight, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return 0, err
	}
	rca := NewRemoteCadenceArch(cdcHeight, e.client, e.config.FlowNetworkID)
	result, err := view.DryCall(
		from,
		to,
		tx.Data,
		tx.Value,
		tx.Gas,
		query.WithExtraPrecompiledContracts([]evmTypes.PrecompiledContract{rca}),
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
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) ([]byte, error) {
	view, err := e.getBlockView(uint64(evmHeight))
	if err != nil {
		return nil, err
	}

	return view.GetCode(address)
}

func (e *EVM) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	val, err := e.executeScriptAtHeight(
		ctx,
		getLatest,
		LatestBlockHeight,
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

// replaceAddresses replace the addresses based on the network
func (e *EVM) replaceAddresses(script []byte) []byte {
	// make the list of all contracts we should replace address for
	sc := systemcontracts.SystemContractsForChain(e.config.FlowNetworkID)
	contracts := []systemcontracts.SystemContract{sc.EVMContract, sc.FungibleToken, sc.FlowToken}

	s := string(script)
	// iterate over all the import name and address pairs and replace them in script
	for _, contract := range contracts {
		s = strings.ReplaceAll(s,
			fmt.Sprintf("import %s", contract.Name),
			fmt.Sprintf("import %s from %s", contract.Name, contract.Address.HexWithPrefix()),
		)
	}

	// also replace COA address if used (in scripts)
	s = strings.ReplaceAll(s, "0xCOA", e.config.COAAddress.HexWithPrefix())

	return []byte(s)
}

// executeScriptAtHeight will execute the given script, at the given
// block height, with the given arguments. A height of `LatestBlockHeight`
// (math.MaxUint64 - 1) is a special value, which means the script will be
// executed at the latest sealed block.
func (e *EVM) executeScriptAtHeight(
	ctx context.Context,
	scriptType scriptType,
	height uint64,
	arguments []cadence.Value,
) (cadence.Value, error) {
	script, ok := scripts[scriptType]
	if !ok {
		return nil, fmt.Errorf("unknown script type")
	}

	// try and get the value from the cache if key is supported
	key := cacheKey(scriptType, height, arguments)
	if key != "" && e.scriptCache != nil {
		val, ok := e.scriptCache.Get(key)
		if ok {
			e.logger.Info().
				Uint64("evm-height", height).
				Int("script", int(scriptType)).
				Str("result", val.String()).
				Msg("cache hit")
			return val, nil
		}
	}

	var res cadence.Value
	var err error

	if height == LatestBlockHeight {
		res, err = e.client.ExecuteScriptAtLatestBlock(
			ctx,
			e.replaceAddresses(script),
			arguments,
		)
	} else {
		res, err = e.client.ExecuteScriptAtBlockHeight(
			ctx,
			height,
			e.replaceAddresses(script),
			arguments,
		)
	}
	if err != nil {
		// if snapshot doesn't exist on EN, the height at which script was executed is out
		// of the boundaries the EN keeps state, so return out of range
		const storageError = "failed to create storage snapshot"
		if strings.Contains(err.Error(), storageError) {
			return nil, errs.NewHeightOutOfRangeError(height)
		}
	} else if key != "" && e.scriptCache != nil { // if error is nil and key is supported add to cache
		e.scriptCache.Add(key, res)
	}

	return res, err
}

func (e *EVM) getBlockView(evmHeight uint64) (*query.View, error) {
	blocksProvider := replayer.NewBlocksProvider(
		e.blocks,
		e.config.FlowNetworkID,
		nil,
	)
	viewProvider := query.NewViewProvider(
		e.config.FlowNetworkID,
		evm.StorageAccountAddress(e.config.FlowNetworkID),
		e.registerStore,
		blocksProvider,
		120_000_000,
	)

	return viewProvider.GetBlockView(evmHeight)
}

// cacheKey builds the cache key from the script type, height and arguments.
func cacheKey(scriptType scriptType, height uint64, args []cadence.Value) string {
	key := fmt.Sprintf("%d%d", scriptType, height)

	switch scriptType {
	case getBalance:
		if len(args) != 1 {
			return ""
		}
		v := args[0].(cadence.String)
		key = fmt.Sprintf("%s%s", key, string(v))
	case getNonce:
		if len(args) != 1 {
			return ""
		}
		v := args[0].(cadence.String)
		key = fmt.Sprintf("%s%s", key, string(v))
	case getLatest:
		// no additional arguments
	default:
		return ""
	}

	return key
}

func AddOne64th(n uint64) uint64 {
	// NOTE: Go's integer division floors, but that is desirable here
	return n + (n / 64)
}

func (e *EVM) evmToCadenceHeight(height int64) (uint64, error) {
	cadenceHeight, err := e.blocks.GetCadenceHeight(uint64(height))
	if err != nil {
		return 0, fmt.Errorf(
			"failed to map evm height: %d to cadence height: %w",
			height,
			err,
		)
	}

	return cadenceHeight, nil
}
