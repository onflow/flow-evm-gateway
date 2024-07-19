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
	"time"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/stdlib"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/txpool"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	errs "github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
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

const minFlowBalance = 2
const coaFundingBalance = minFlowBalance - 1

const LatestBlockHeight uint64 = math.MaxUint64 - 1

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
	Call(ctx context.Context, data []byte, from common.Address, evmHeight int64) ([]byte, error)

	// EstimateGas executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make any changes in the state/blockchain and is
	// useful to executed and retrieve the gas consumption and possible failures.
	EstimateGas(ctx context.Context, data []byte, from common.Address, evmHeight int64) (uint64, error)

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
	client *CrossSporkClient
	config *config.Config
	signer crypto.Signer
	txPool *TxPool
	logger zerolog.Logger
	blocks storage.BlockIndexer

	head              *types.Header
	evmSigner         types.Signer
	validationOptions *txpool.ValidationOptions

	collector metrics.Collector
}

func NewEVM(
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

	evm := &EVM{
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
		// we ignore errors for now since creation of already existing COA resource will fail, which is fine for now
		tx, err := evm.buildTransaction(
			context.Background(),
			evm.replaceAddresses(createCOAScript),
			cadence.UFix64(coaFundingBalance),
		)
		if err != nil {
			logger.Warn().Err(err).Msg("COA resource auto-creation failure")
		}
		if err := evm.client.SendTransaction(context.Background(), *tx); err != nil {
			logger.Warn().Err(err).Msg("failed to send COA resource auto-creation transaction")
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
		return common.Hash{}, errs.NewErrGasPriceTooLow(e.config.GasPrice)
	}

	txData := hex.EncodeToString(data)
	hexEncodedTx, err := cadence.NewString(txData)
	if err != nil {
		return common.Hash{}, err
	}

	script := e.replaceAddresses(runTxScript)
	flowTx, err := e.buildTransaction(ctx, script, hexEncodedTx)
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
		e.collector.EvmAccountCalled(prometheus.Labels{"address": to})
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
			return nil, fmt.Errorf("failed to add argument: %w", err)
		}
	}

	if err := flowTx.SignEnvelope(address, index, e.signer); err != nil {
		return nil, fmt.Errorf("failed to sign transaction envelope: %w", err)
	}

	return flowTx, nil
}

func (e *EVM) GetBalance(
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
		getBalanceScript,
		height,
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, ErrOutOfRange) {
			e.logger.Error().
				Err(err).
				Str("address", address.String()).
				Int64("evm-height", evmHeight).
				Uint64("cadence-height", height).
				Msg("failed to get get balance")
		}
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt); !ok {
		e.logger.Panic().Msg(fmt.Sprintf("failed to convert balance %v to UInt", val))
	}

	return val.(cadence.UInt).Big(), nil
}

func (e *EVM) GetNonce(
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
		getNonceScript,
		height,
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, ErrOutOfRange) {
			e.logger.Error().Err(err).
				Str("address", address.String()).
				Int64("evm-height", evmHeight).
				Uint64("cadence-height", height).
				Msg("failed to get nonce")
		}
		return 0, fmt.Errorf("failed to get nonce: %w", err)
	}

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt64); !ok {
		e.logger.Panic().Msg(fmt.Sprintf("failed to convert balance %v to UInt64", val))
	}

	nonce := uint64(val.(cadence.UInt64))

	e.logger.Debug().
		Uint64("nonce", nonce).
		Int64("evm-height", evmHeight).
		Uint64("cadence-height", height).
		Msg("get nonce executed")

	return nonce, nil
}

func (e *EVM) stateAt(evmHeight int64) (*state.StateDB, error) {
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

	ledger, err := newRemoteLedger(e.config.AccessNodeHost, cadenceHeight)
	if err != nil {
		return nil, fmt.Errorf("could not create a remote ledger: %w", err)
	}

	return state.NewStateDB(ledger, previewnetStorageAddress)
}

func (e *EVM) GetStorageAt(
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

func (e *EVM) Call(
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
		dryRunScript,
		height,
		[]cadence.Value{hexEncodedTx, hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, ErrOutOfRange) {
			e.logger.Error().
				Err(err).
				Uint64("cadence-height", height).
				Int64("evm-height", evmHeight).
				Str("from", from.String()).
				Str("data", string(data)).
				Msg("failed to execute call")
		}
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	evmResult, err := stdlib.ResultSummaryFromEVMResultValue(scriptResult)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EVM result from call [%s]: %w", scriptResult.String(), err)
	}

	if evmResult.ErrorCode != 0 {
		if evmResult.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return nil, errs.NewRevertError(evmResult.ReturnedData)
		}
		return nil, evmTypes.ErrorFromCode(evmResult.ErrorCode)
	}

	result := evmResult.ReturnedData

	e.logger.Debug().
		Str("result", hex.EncodeToString(result)).
		Int64("evm-height", evmHeight).
		Uint64("cadence-height", height).
		Msg("call executed")

	return result, nil
}

func (e *EVM) EstimateGas(
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
		dryRunScript,
		height,
		[]cadence.Value{hexEncodedTx, hexEncodedAddress},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to execute script: %w", err)
	}

	evmResult, err := stdlib.ResultSummaryFromEVMResultValue(scriptResult)
	if err != nil {
		return 0, fmt.Errorf("failed to decode EVM result from gas estimation: %w", err)
	}

	if evmResult.ErrorCode != 0 {
		if evmResult.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return 0, errs.NewRevertError(evmResult.ReturnedData)
		}
		return 0, evmTypes.ErrorFromCode(evmResult.ErrorCode)
	}

	gasConsumed := evmResult.GasConsumed

	e.logger.Debug().
		Uint64("gas", gasConsumed).
		Msg("gas estimation executed")

	return gasConsumed, nil
}

func (e *EVM) GetCode(
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
		getCodeScript,
		height,
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		if !errors.Is(err, ErrOutOfRange) {
			e.logger.Error().
				Err(err).
				Uint64("cadence-height", height).
				Int64("evm-height", evmHeight).
				Str("address", address.String()).
				Msg("failed to get code")
		}

		return nil, fmt.Errorf("failed to execute script for get code: %w", err)
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

func (e *EVM) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	// TODO(m-Peter): Consider adding some time-based caching, if this
	// endpoint turns out to be called quite frequently.
	val, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		e.replaceAddresses(getLatestEVMHeight),
		[]cadence.Value{},
	)
	if err != nil {
		return 0, err
	}

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt64); !ok {
		e.logger.Panic().Msg(fmt.Sprintf("failed to convert height %v to UInt64", val))
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
		return 0, 0, fmt.Errorf("failed to get signer info account: %w", err)
	}

	signerPub := e.signer.PublicKey()
	for _, k := range account.Keys {
		if k.PublicKey.Equals(signerPub) {
			return k.Index, k.SequenceNumber, nil
		}
	}

	return 0, 0, fmt.Errorf("provided account address and signer keys do not match")
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

func (e *EVM) evmToCadenceHeight(height int64) (uint64, error) {
	if height < 0 {
		return LatestBlockHeight, nil
	}

	evmHeight := uint64(height)
	evmLatest, err := e.blocks.LatestEVMHeight()
	if err != nil {
		return 0, fmt.Errorf("failed to map evm to cadence height, getting latest evm height: %w", err)
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
		return 0, fmt.Errorf("failed to map evm to cadence height: %w", err)
	}

	return cadenceHeight, nil
}

// executeScriptAtHeight will execute the given script, at the given
// block height, with the given arguments. A height of `LatestBlockHeight`
// (math.MaxUint64 - 1) is a special value, which means the script will be
// executed at the latest sealed block.
func (e *EVM) executeScriptAtHeight(
	ctx context.Context,
	script []byte,
	height uint64,
	arguments []cadence.Value,
) (cadence.Value, error) {
	if height == LatestBlockHeight {
		return e.client.ExecuteScriptAtLatestBlock(
			ctx,
			e.replaceAddresses(script),
			arguments,
		)
	}

	res, err := e.client.ExecuteScriptAtBlockHeight(
		ctx,
		height,
		e.replaceAddresses(script),
		arguments,
	)
	if err != nil {
		// if snapshot doesn't exist on EN, the height at which script was executed is out
		// of the boundaries the EN keeps state, so return out of range
		if strings.Contains(err.Error(), "failed to create storage snapshot") {
			return nil, ErrOutOfRange
		}
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
		return nil, fmt.Errorf("failed to convert cadence value to string: %v", value)
	}

	code, err := hex.DecodeString(string(cdcString))
	if err != nil {
		return nil, fmt.Errorf("failed to decode string to byte array [%s]: %w", cdcString, err)
	}

	return code, nil
}
