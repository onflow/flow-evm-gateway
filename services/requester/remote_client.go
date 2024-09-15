package requester

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	evmImpl "github.com/onflow/flow-go/fvm/evm/impl"
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
	"github.com/onflow/flow-evm-gateway/storage"
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

var _ EVMClient = &RemoteClient{}

type RemoteClient struct {
	client      *CrossSporkClient
	config      *config.Config
	signer      crypto.Signer
	txPool      *TxPool
	logger      zerolog.Logger
	blocks      storage.BlockIndexer
	mux         sync.Mutex
	scriptCache *expirable.LRU[string, cadence.Value]

	head              *types.Header
	evmSigner         types.Signer
	validationOptions *txpool.ValidationOptions

	collector metrics.Collector
}

func NewRemote(
	client *CrossSporkClient,
	config *config.Config,
	signer crypto.Signer,
	logger zerolog.Logger,
	blocks storage.BlockIndexer,
	txPool *TxPool,
	collector metrics.Collector,
) (*RemoteClient, error) {
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

	evm := &RemoteClient{
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

func (e *RemoteClient) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
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
func (e *RemoteClient) buildTransaction(ctx context.Context, script []byte, args ...cadence.Value) (*flow.Transaction, error) {
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

func (e *RemoteClient) GetBalance(
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) (*big.Int, error) {
	hexEncodedAddress, err := addressToCadenceString(address)
	if err != nil {
		return nil, err
	}

	height, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	val, err := e.executeScriptAtHeight(
		ctx,
		getBalance,
		height,
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, errs.ErrHeightOutOfRange) {
			e.logger.Error().
				Err(err).
				Str("address", address.String()).
				Int64("evm-height", evmHeight).
				Uint64("cadence-height", height).
				Msg("failed to get get balance")
		}
		return nil, fmt.Errorf(
			"failed to get balance of address: %s at height: %d, with: %w",
			address,
			evmHeight,
			err,
		)
	}

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt); !ok {
		return nil, fmt.Errorf("failed to convert balance %v to UInt, got type: %T", val, val)
	}

	return val.(cadence.UInt).Big(), nil
}

func (e *RemoteClient) GetNonce(
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) (uint64, error) {
	hexEncodedAddress, err := addressToCadenceString(address)
	if err != nil {
		return 0, err
	}

	height, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return 0, err
	}

	val, err := e.executeScriptAtHeight(
		ctx,
		getNonce,
		height,
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, errs.ErrHeightOutOfRange) {
			e.logger.Error().Err(err).
				Str("address", address.String()).
				Int64("evm-height", evmHeight).
				Uint64("cadence-height", height).
				Msg("failed to get nonce")
		}
		return 0, fmt.Errorf(
			"failed to get nonce of address: %s at height: %d, with: %w",
			address,
			evmHeight,
			err,
		)
	}

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt64); !ok {
		return 0, fmt.Errorf("failed to convert nonce %v to UInt64, got type: %T", val, val)
	}

	nonce := uint64(val.(cadence.UInt64))

	e.logger.Debug().
		Uint64("nonce", nonce).
		Int64("evm-height", evmHeight).
		Uint64("cadence-height", height).
		Msg("get nonce executed")

	return nonce, nil
}

func (e *RemoteClient) stateAt(evmHeight int64) (*state.StateDB, error) {
	cadenceHeight, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	if cadenceHeight == LatestBlockHeight {
		h, err := e.client.GetLatestBlockHeader(context.Background(), true)
		if err != nil {
			return nil, err
		}
		cadenceHeight = h.Height
	}

	exeClient, ok := e.client.Client.(*grpc.Client)
	if !ok {
		return nil, fmt.Errorf("could not convert to execution client")
	}
	ledger, err := newRemoteLedger(exeClient.ExecutionDataRPCClient(), cadenceHeight)
	if err != nil {
		return nil, fmt.Errorf("could not create remote ledger for height: %d, with: %w", cadenceHeight, err)
	}

	storageAddress := evm.StorageAccountAddress(e.config.FlowNetworkID)
	return state.NewStateDB(ledger, storageAddress)
}

func (e *RemoteClient) GetStorageAt(
	ctx context.Context,
	address common.Address,
	hash common.Hash,
	evmHeight int64,
) (common.Hash, error) {
	stateDB, err := e.stateAt(evmHeight)
	if err != nil {
		return common.Hash{}, err
	}

	result := stateDB.GetState(address, hash)
	return result, stateDB.Error()
}

func (e *RemoteClient) Call(
	ctx context.Context,
	data []byte,
	from common.Address,
	evmHeight int64,
) ([]byte, error) {
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(data))
	if err != nil {
		return nil, err
	}

	hexEncodedAddress, err := addressToCadenceString(from)
	if err != nil {
		return nil, err
	}

	height, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	scriptResult, err := e.executeScriptAtHeight(
		ctx,
		dryRun,
		height,
		[]cadence.Value{hexEncodedTx, hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, errs.ErrHeightOutOfRange) {
			e.logger.Error().
				Err(err).
				Uint64("cadence-height", height).
				Int64("evm-height", evmHeight).
				Str("from", from.String()).
				Str("data", hex.EncodeToString(data)).
				Msg("failed to execute call")
		}
		return nil, fmt.Errorf("failed to execute script at height: %d, with: %w", height, err)
	}

	evmResult, err := parseResult(scriptResult)
	if err != nil {
		return nil, err
	}

	result := evmResult.ReturnedData

	e.logger.Debug().
		Str("result", hex.EncodeToString(result)).
		Int64("evm-height", evmHeight).
		Uint64("cadence-height", height).
		Msg("call executed")

	return result, nil
}

func (e *RemoteClient) EstimateGas(
	ctx context.Context,
	data []byte,
	from common.Address,
	evmHeight int64,
) (uint64, error) {
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(data))
	if err != nil {
		return 0, err
	}

	hexEncodedAddress, err := addressToCadenceString(from)
	if err != nil {
		return 0, err
	}

	height, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return 0, err
	}

	scriptResult, err := e.executeScriptAtHeight(
		ctx,
		dryRun,
		height,
		[]cadence.Value{hexEncodedTx, hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, errs.ErrHeightOutOfRange) {
			e.logger.Error().
				Err(err).
				Uint64("cadence-height", height).
				Int64("evm-height", evmHeight).
				Str("from", from.String()).
				Str("data", hex.EncodeToString(data)).
				Msg("failed to execute estimateGas")
		}
		return 0, fmt.Errorf("failed to execute script at height: %d, with: %w", height, err)
	}

	evmResult, err := parseResult(scriptResult)
	if err != nil {
		return 0, err
	}

	gasConsumed := evmResult.GasConsumed

	e.logger.Debug().
		Uint64("gas", gasConsumed).
		Int64("evm-height", evmHeight).
		Uint64("cadence-height", height).
		Msg("estimateGas executed")

	return gasConsumed, nil
}

func (e *RemoteClient) GetCode(
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) ([]byte, error) {
	hexEncodedAddress, err := addressToCadenceString(address)
	if err != nil {
		return nil, err
	}

	height, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	value, err := e.executeScriptAtHeight(
		ctx,
		getCode,
		height,
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, errs.ErrHeightOutOfRange) {
			e.logger.Error().
				Err(err).
				Uint64("cadence-height", height).
				Int64("evm-height", evmHeight).
				Str("address", address.String()).
				Msg("failed to get code")
		}

		return nil, fmt.Errorf(
			"failed to execute script for get code of address: %s at height: %d, with: %w",
			address,
			height,
			err,
		)
	}

	code, err := cadenceStringToBytes(value)
	if err != nil {
		return nil, err
	}

	e.logger.Debug().
		Str("address", address.Hex()).
		Int64("evm-height", evmHeight).
		Uint64("cadence-height", height).
		Str("code size", fmt.Sprintf("%d", len(code))).
		Msg("get code executed")

	return code, nil
}

func (e *RemoteClient) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
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
func (e *RemoteClient) getSignerNetworkInfo(ctx context.Context) (uint32, uint64, error) {
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
func (e *RemoteClient) replaceAddresses(script []byte) []byte {
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

func (e *RemoteClient) evmToCadenceHeight(height int64) (uint64, error) {
	if height < 0 {
		return LatestBlockHeight, nil
	}

	evmHeight := uint64(height)
	evmLatest, err := e.blocks.LatestEVMHeight()
	if err != nil {
		return 0, fmt.Errorf(
			"failed to map evm height: %d to cadence height, getting latest evm height: %w",
			evmHeight,
			err,
		)
	}

	// if provided evm height equals to latest evm height indexed we
	// return latest height special value to signal requester to execute
	// script at the latest block, not at the cadence height we get from the
	// index, that is because at that point the height might already be pruned
	if evmHeight == evmLatest {
		return LatestBlockHeight, nil
	}

	cadenceHeight, err := e.blocks.GetCadenceHeight(uint64(evmHeight))
	if err != nil {
		return 0, fmt.Errorf("failed to map evm height: %d to cadence height: %w", evmHeight, err)
	}

	return cadenceHeight, nil
}

// executeScriptAtHeight will execute the given script, at the given
// block height, with the given arguments. A height of `LatestBlockHeight`
// (math.MaxUint64 - 1) is a special value, which means the script will be
// executed at the latest sealed block.
func (e *RemoteClient) executeScriptAtHeight(
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

func addressToCadenceString(address common.Address) (cadence.String, error) {
	return cadence.NewString(
		strings.TrimPrefix(address.Hex(), "0x"),
	)
}

func cadenceStringToBytes(value cadence.Value) ([]byte, error) {
	cdcString, ok := value.(cadence.String)
	if !ok {
		return nil, fmt.Errorf(
			"failed to convert cadence value of type: %T to string: %v",
			value,
			value,
		)
	}

	if cdcString == "" {
		return nil, nil
	}

	code, err := hex.DecodeString(string(cdcString))
	if err != nil {
		return nil, fmt.Errorf("failed to hex-decode string to byte array [%s]: %w", cdcString, err)
	}

	return code, nil
}

// parseResult
func parseResult(res cadence.Value) (*evmTypes.ResultSummary, error) {
	result, err := evmImpl.ResultSummaryFromEVMResultValue(res)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EVM result of type: %s, with: %w", res.Type().ID(), err)
	}

	if result.ErrorCode != 0 {
		if result.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return nil, errs.NewRevertError(result.ReturnedData)
		}
		return nil, errs.NewFailedTransactionError(result.ErrorMessage)
	}

	return result, err
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
