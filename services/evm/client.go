package evm

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

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
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
	//go:embed cadence/run.cdc
	runTxScript []byte

	//go:embed cadence/create_coa.cdc
	createCOAScript []byte

	//go:embed cadence/get_latest_evm_height.cdc
	getLatestEVMHeight []byte
)

const minFlowBalance = 2
const coaFundingBalance = minFlowBalance - 1

const LatestBlockHeight uint64 = math.MaxUint64 - 1

type EVMClient interface {
	// todo submitting transactions should be extracted in another type, this EVM client should only be used for
	// querying the state and executing calls and gas estimations, the transaction submission should entirely fall
	// in the domain of the FlowClient (a new type), whereas EVMClient as the name implies should only handle
	// EVM requests

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

var _ EVMClient = &RemoteClient{}

type RemoteClient struct {
	client         *CrossSporkClient
	config         *config.Config
	signer         crypto.Signer
	txPool         *TxPool
	logger         zerolog.Logger
	blocks         storage.BlockIndexer
	mux            sync.Mutex
	storageAccount flowGo.Address

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

	evmClient := &RemoteClient{
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
		storageAccount:    evm.StorageAccountAddress(config.FlowNetworkID),
	}

	// create COA on the account
	if config.CreateCOAResource {
		tx, err := evmClient.buildTransaction(
			context.Background(),
			evmClient.replaceAddresses(createCOAScript),
			cadence.UFix64(coaFundingBalance),
		)
		if err != nil {
			logger.Warn().Err(err).Msg("COA resource auto-creation failure")
			return nil, fmt.Errorf("COA resource auto-creation failure: %w", err)
		}
		if err := evmClient.client.SendTransaction(context.Background(), *tx); err != nil {
			logger.Warn().Err(err).Msg("failed to send COA resource auto-creation transaction")
			return nil, fmt.Errorf("failed to send COA resource auto-creation transaction: %w", err)
		}
	}

	return evmClient, nil
}

// todo move to flow client
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
	stateDB, err := e.stateAt(evmHeight)
	if err != nil {
		return nil, err
	}

	return stateDB.GetBalance(address).ToBig(), stateDB.Error()
}

func (e *RemoteClient) GetNonce(
	ctx context.Context,
	address common.Address,
	evmHeight int64,
) (uint64, error) {
	stateDB, err := e.stateAt(evmHeight)
	if err != nil {
		return 0, err
	}

	return stateDB.GetNonce(address), stateDB.Error()
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

	return stateDB.GetState(address, hash), stateDB.Error()
}

func (e *RemoteClient) GetCode(ctx context.Context, address common.Address, evmHeight int64) ([]byte, error) {
	stateDB, err := e.stateAt(evmHeight)
	if err != nil {
		return nil, err
	}

	return stateDB.GetCode(address), stateDB.Error()
}

func (e *RemoteClient) EstimateGas(ctx context.Context, data []byte, from common.Address, evmHeight int64) (uint64, error) {
	executor, err := e.executorAt(evmHeight)
	if err != nil {
		return 0, err
	}

	res, err := executor.Call(from, data)
	if err != nil {
		return 0, err
	}

	result := res.ResultSummary()
	if result.ErrorCode != 0 {
		if result.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return 0, errs.NewRevertError(result.ReturnedData)
		}
		return 0, errs.NewFailedTransactionError(result.ErrorMessage)
	}

	return res.GasConsumed, nil
}

func (e *RemoteClient) Call(
	ctx context.Context,
	data []byte,
	from common.Address,
	evmHeight int64,
) ([]byte, error) {
	executor, err := e.executorAt(evmHeight)
	if err != nil {
		return nil, err
	}

	res, err := executor.Call(from, data)
	if err != nil {
		return nil, err
	}

	result := res.ResultSummary()
	if result.ErrorCode != 0 {
		if result.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return nil, errs.NewRevertError(result.ReturnedData)
		}
		return nil, errs.NewFailedTransactionError(result.ErrorMessage)
	}

	// make sure the nil returned data is returned as empty slice to match remote client
	if res.ReturnedData == nil {
		res.ReturnedData = make([]byte, 0)
	}

	return res.ReturnedData, nil
}

// todo move to flow client
func (e *RemoteClient) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	val, err := e.executeScriptAtHeight(
		ctx,
		getLatestEVMHeight,
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

func (e *RemoteClient) resolveEVMHeight(height int64) (uint64, error) {
	evmHeight := uint64(height)

	// if height is special value latest height
	if height < 0 {
		h, err := e.blocks.LatestEVMHeight()
		if err != nil {
			return 0, err
		}
		evmHeight = h
	}

	return evmHeight, nil
}

func (e *RemoteClient) evmToCadenceHeight(height int64) (uint64, error) {
	evmHeight, err := e.resolveEVMHeight(height)
	if err != nil {
		return 0, err
	}

	cadenceHeight, err := e.blocks.GetCadenceHeight(evmHeight)
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
	script []byte,
	height uint64,
	arguments []cadence.Value,
) (cadence.Value, error) {
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

	return res, err
}

func (e *RemoteClient) stateAt(evmHeight int64) (evmTypes.StateDB, error) {
	ledger, err := e.ledgerAt(evmHeight)
	if err != nil {
		return nil, err
	}

	return state.NewStateDB(ledger, e.storageAccount)
}

func (e *RemoteClient) ledgerAt(evmHeight int64) (*remoteLedger, error) {
	cadenceHeight, err := e.evmToCadenceHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	client, err := e.client.getClientForHeight(cadenceHeight)
	if err != nil {
		return nil, err
	}

	exeClient, ok := client.(*grpc.Client)
	if !ok {
		return nil, fmt.Errorf("could not convert to execution client")
	}
	ledger, err := newRemoteLedger(exeClient.ExecutionDataRPCClient(), cadenceHeight)
	if err != nil {
		return nil, fmt.Errorf("could not create remote ledger for height: %d, with: %w", cadenceHeight, err)
	}

	return ledger, nil
}

func (e *RemoteClient) executorAt(evmHeight int64) (*BlockExecutor, error) {
	ledger, err := e.ledgerAt(evmHeight)
	if err != nil {
		return nil, err
	}

	height, err := e.resolveEVMHeight(evmHeight)
	if err != nil {
		return nil, err
	}

	block, err := e.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	return NewBlockExecutor(block, ledger, e.config.FlowNetworkID, e.blocks, e.logger)
}
