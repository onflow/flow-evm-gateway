package requester

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-retry"
	"golang.org/x/sync/errgroup"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

const (
	evmErrorRegex = `evm_error=(.*)\n`
)

// TxPool is the minimum interface that needs to be implemented by
// the various transaction pool strategies.
type TxPool interface {
	Add(ctx context.Context, tx *gethTypes.Transaction) error
}

// SingleTxPool is a simple implementation of the `TxPool` interface that submits
// transactions as soon as they arrive, without any delays or batching strategies.
type SingleTxPool struct {
	logger      zerolog.Logger
	client      *CrossSporkClient
	pool        *sync.Map
	txPublisher *models.Publisher[*gethTypes.Transaction]
	config      config.Config
	mux         sync.Mutex
	keystore    *keystore.KeyStore
	collector   metrics.Collector
	// todo add methods to inspect transaction pool state
}

var _ TxPool = &SingleTxPool{}

func NewSingleTxPool(
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
) *SingleTxPool {
	// initialize the available keys metric since it is only updated when sending a tx
	collector.AvailableSigningKeys(keystore.AvailableKeys())

	return &SingleTxPool{
		logger:      logger.With().Str("component", "tx-pool").Logger(),
		client:      client,
		txPublisher: transactionsPublisher,
		pool:        &sync.Map{},
		config:      config,
		collector:   collector,
		keystore:    keystore,
	}
}

// Add creates a Cadence transaction that wraps the given EVM transaction in
// an `EVM.run` function call for execution.
//
// The Cadence transaction is submitted to the Flow network right away.
//
// If the transaction state validation is configured to run with the
// "tx-seal" strategy, the Cadence transaction status is awaited and an error
// is returned in case of a failure in submission or an EVM validation error.
// Until the Cadence transaction is sealed the transaction will stay in the
// pool and marked as pending.
//
// If the transaction state validation is configured to run with the
// "local-index" strategy, the Cadence transaction status is not awaited,
// as the necessary EVM validation checks, such as nonce/balance checks,
// have been checked against the EVM state of the local index.
func (t *SingleTxPool) Add(
	ctx context.Context,
	tx *gethTypes.Transaction,
) error {
	t.txPublisher.Publish(tx) // publish pending transaction event

	txData, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(txData))
	if err != nil {
		return err
	}
	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return err
	}

	script := replaceAddresses(runTxScript, t.config.FlowNetworkID)
	flowTx, err := t.buildTransaction(ctx, script, hexEncodedTx, coinbaseAddress)
	if err != nil {
		return err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		return err
	}

	if t.config.TxStateValidation == config.TxSealValidation {
		// add to pool and delete after transaction is sealed or errored out
		t.pool.Store(tx.Hash(), tx)
		defer t.pool.Delete(tx.Hash())

		backoff := retry.WithMaxDuration(time.Minute*1, retry.NewConstant(time.Second*1))
		return retry.Do(ctx, backoff, func(ctx context.Context) error {
			res, err := t.client.GetTransactionResult(ctx, flowTx.ID())
			if err != nil {
				return fmt.Errorf("failed to retrieve flow transaction result %s: %w", flowTx.ID(), err)
			}
			// retry until transaction is sealed
			if res.Status < flow.TransactionStatusSealed {
				return retry.RetryableError(fmt.Errorf("transaction %s not sealed", flowTx.ID()))
			}

			if res.Error != nil {
				if err, ok := parseInvalidError(res.Error); ok {
					return err
				}

				t.logger.Error().Err(res.Error).
					Str("flow-id", flowTx.ID().String()).
					Str("evm-id", tx.Hash().Hex()).
					Msg("flow transaction error")

				// hide specific cause since it's an implementation issue
				return fmt.Errorf("failed to submit flow evm transaction %s", tx.Hash())
			}

			return nil
		})
	}

	return nil
}

// buildTransaction creates a Cadence transaction from the provided script,
// with the given arguments and signs it with the configured COA account.
func (t *SingleTxPool) buildTransaction(
	ctx context.Context,
	script []byte,
	args ...cadence.Value,
) (*flow.Transaction, error) {
	defer func() {
		t.collector.AvailableSigningKeys(t.keystore.AvailableKeys())
	}()

	var (
		g           = errgroup.Group{}
		err1, err2  error
		latestBlock *flow.Block
		account     *flow.Account
	)
	// execute concurrently so we can speed up all the information we need for tx
	g.Go(func() error {
		latestBlock, err1 = t.client.GetLatestBlock(ctx, true)
		return err1
	})
	g.Go(func() error {
		account, err2 = t.client.GetAccount(ctx, t.config.COAAddress)
		return err2
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	flowTx := flow.NewTransaction().
		SetScript(script).
		SetReferenceBlockID(latestBlock.ID).
		SetComputeLimit(flowGo.DefaultMaxTransactionGasLimit)

	for _, arg := range args {
		if err := flowTx.AddArgument(arg); err != nil {
			return nil, fmt.Errorf("failed to add argument: %s, with %w", arg, err)
		}
	}

	// building and signing transactions should be blocking,
	// so we don't have keys conflict
	t.mux.Lock()
	defer t.mux.Unlock()

	accKey, err := t.keystore.Take()
	if err != nil {
		return nil, err
	}

	if err := accKey.SetProposerPayerAndSign(flowTx, account); err != nil {
		accKey.Done()
		return nil, err
	}

	// now that the transaction is prepared, store the transaction's metadata
	accKey.SetLockMetadata(flowTx.ID(), latestBlock.Height)

	t.collector.OperatorBalance(account)

	return flowTx, nil
}

type pooledEvmTx struct {
	txPayload cadence.String
	nonce     uint64
}

// BatchTxPool is a `TxPool` implementation that collects and groups
// transactions based on their EOA signer, and submits them for execution
// using a batch.
//
// The underlying Cadence EVM API used, is `EVM.batchRun`, instead of the
// `EVM.run` used in `SingleTxPool`.
//
// The main advantage of this implementation over the `SingleTxPool`, is the
// guarantee that transactions originated from the same EOA address, which
// arrive in a short time interval (about the same as Flow's block productionrate),
// will be executed in the same order their arrived.
// This helps to reduce the nonce mismatch errors which mainly occur from the
// re-ordering of Cadence transactions that happens from Collection nodes.
type BatchTxPool struct {
	*SingleTxPool
	pooledTxs map[gethCommon.Address][]pooledEvmTx
	txMux     sync.Mutex
}

var _ TxPool = &BatchTxPool{}

func NewBatchTxPool(
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
) *BatchTxPool {
	// initialize the available keys metric since it is only updated when sending a tx
	collector.AvailableSigningKeys(keystore.AvailableKeys())

	singleTxPool := NewSingleTxPool(
		client,
		transactionsPublisher,
		logger,
		config,
		collector,
		keystore,
	)
	batchPool := &BatchTxPool{
		SingleTxPool: singleTxPool,
		pooledTxs:    make(map[gethCommon.Address][]pooledEvmTx),
		txMux:        sync.Mutex{},
	}

	go batchPool.processPooledTransactions()

	return batchPool
}

// Add adds the EVM transaction to the tx pool, grouped with the rest of the
// transactions from the same EOA signer.
// After the configured `TxBatchInterval`, the collected transations
// are batched and sent to the Flow network using `EVM.batchRun`, for execution.
func (t *BatchTxPool) Add(
	ctx context.Context,
	tx *gethTypes.Transaction,
) error {
	t.txPublisher.Publish(tx) // publish pending transaction event

	// tx sending should be blocking, so we don't have races when
	// pooled transactions are being processed in the background.
	t.txMux.Lock()
	defer t.txMux.Unlock()

	from, err := gethTypes.Sender(gethTypes.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return fmt.Errorf("failed to derive the sender: %w", err)
	}
	txData, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(txData))
	if err != nil {
		return err
	}

	userTx := pooledEvmTx{txPayload: hexEncodedTx, nonce: tx.Nonce()}
	t.pooledTxs[from] = append(t.pooledTxs[from], userTx)

	return nil
}

func (t *BatchTxPool) processPooledTransactions() {
	for range time.Tick(t.config.TxBatchInterval) {
		latestBlock, account, err := t.fetchFlowLatestBlockAndCOA()
		if err != nil {
			t.logger.Error().Err(err).Msg(
				"failed to get COA / latest Flow block on batch tx submission",
			)
			continue
		}

		// Take a snapshot here to allow `Add()` to continue accept
		// incoming EVM transactions, without blocking until the
		// batch transactions are submitted.
		txsGroupedByAddress := func() map[gethCommon.Address][]pooledEvmTx {
			t.txMux.Lock()
			defer t.txMux.Unlock()
			copy := t.pooledTxs
			t.pooledTxs = make(map[gethCommon.Address][]pooledEvmTx)
			return copy
		}()

		for address, pooledTxs := range txsGroupedByAddress {
			err := t.batchSubmitTransactionsForSameAddress(
				latestBlock,
				account,
				pooledTxs,
			)
			if err != nil {
				t.logger.Error().Err(err).Msgf(
					"failed to send Flow transaction from BatchPool for EOA: %s",
					address.Hex(),
				)
				continue
			}
		}
	}
}

func (t *BatchTxPool) batchSubmitTransactionsForSameAddress(
	latestBlock *flow.Block,
	account *flow.Account,
	pooledTxs []pooledEvmTx,
) error {
	// Sort the transactions based on their nonce, to make sure
	// that no re-ordering has happened due to races etc.
	sort.Slice(pooledTxs, func(i, j int) bool {
		return pooledTxs[i].nonce < pooledTxs[j].nonce
	})

	hexEncodedTxs := make([]cadence.Value, len(pooledTxs))
	for i, txPayload := range pooledTxs {
		hexEncodedTxs[i] = txPayload.txPayload
	}

	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return err
	}

	script := replaceAddresses(batchRunTxScript, t.config.FlowNetworkID)
	flowTx, err := t.buildTransaction(
		latestBlock,
		account,
		script,
		cadence.NewArray(hexEncodedTxs),
		coinbaseAddress,
	)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		return err
	}

	return nil
}

// buildTransaction creates a Cadence transaction from the provided script,
// with the given arguments and signs it with the configured COA account.
func (t *BatchTxPool) buildTransaction(
	latestBlock *flow.Block,
	account *flow.Account,
	script []byte,
	args ...cadence.Value,
) (*flow.Transaction, error) {
	defer func() {
		t.collector.AvailableSigningKeys(t.keystore.AvailableKeys())
	}()

	flowTx := flow.NewTransaction().
		SetScript(script).
		SetReferenceBlockID(latestBlock.ID).
		SetComputeLimit(flowGo.DefaultMaxTransactionGasLimit)

	for _, arg := range args {
		if err := flowTx.AddArgument(arg); err != nil {
			return nil, fmt.Errorf("failed to add argument: %s, with %w", arg, err)
		}
	}

	// building and signing transactions should be blocking,
	// so we don't have keys conflict
	t.mux.Lock()
	defer t.mux.Unlock()

	accKey, err := t.keystore.Take()
	if err != nil {
		return nil, err
	}

	if err := accKey.SetProposerPayerAndSign(flowTx, account); err != nil {
		accKey.Done()
		return nil, err
	}

	// now that the transaction is prepared, store the transaction's metadata
	accKey.SetLockMetadata(flowTx.ID(), latestBlock.Height)

	t.collector.OperatorBalance(account)

	return flowTx, nil
}

func (t *BatchTxPool) fetchFlowLatestBlockAndCOA() (
	*flow.Block,
	*flow.Account,
	error,
) {
	var (
		g           = errgroup.Group{}
		err1, err2  error
		latestBlock *flow.Block
		account     *flow.Account
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// execute concurrently so we can speed up all the information we need for tx
	g.Go(func() error {
		latestBlock, err1 = t.client.GetLatestBlock(ctx, true)
		return err1
	})
	g.Go(func() error {
		account, err2 = t.client.GetAccount(ctx, t.config.COAAddress)
		return err2
	})
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return latestBlock, account, nil
}

// this will extract the evm specific error from the Flow transaction error message
// the run.cdc script panics with the evm specific error as the message which we
// extract and return to the client. Any error returned that is evm specific
// is a validation error due to assert statement in the run.cdc script.
func parseInvalidError(err error) (error, bool) {
	r := regexp.MustCompile(evmErrorRegex)
	matches := r.FindStringSubmatch(err.Error())
	if len(matches) != 2 {
		return nil, false
	}

	return errs.NewFailedTransactionError(matches[1]), true
}
