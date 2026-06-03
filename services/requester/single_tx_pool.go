package requester

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-retry"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

const referenceBlockUpdateFrequency = time.Second * 15

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
	// referenceBlockHeader is stored atomically to avoid races
	// between request path and ticker updates.
	referenceBlockHeader atomic.Value // stores *flow.BlockHeader
	// todo add methods to inspect transaction pool state
}

var _ TxPool = &SingleTxPool{}

func NewSingleTxPool(
	ctx context.Context,
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
) (*SingleTxPool, error) {
	referenceBlockHeader, err := client.GetLatestBlockHeader(ctx, false)
	if err != nil {
		return nil, err
	}

	// initialize the available keys metric since it is only updated when sending a tx
	collector.AvailableSigningKeys(keystore.AvailableKeys())

	singleTxPool := &SingleTxPool{
		logger:      logger.With().Str("component", "tx-pool").Logger(),
		client:      client,
		txPublisher: transactionsPublisher,
		pool:        &sync.Map{},
		config:      config,
		collector:   collector,
		keystore:    keystore,
	}
	singleTxPool.referenceBlockHeader.Store(referenceBlockHeader)

	go singleTxPool.updateReferenceBlock(ctx)

	return singleTxPool, nil
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
	flowTx, err := t.buildTransaction(
		ctx,
		t.getReferenceBlock(),
		script,
		cadence.NewArray([]cadence.Value{hexEncodedTx}),
		coinbaseAddress,
	)
	if err != nil {
		// If there was any error during the transaction build
		// process, we record it as a dropped transaction.
		t.collector.TransactionsDropped(1)
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
	referenceBlockHeader *flow.BlockHeader,
	script []byte,
	args ...cadence.Value,
) (*flow.Transaction, error) {
	defer func() {
		t.collector.AvailableSigningKeys(t.keystore.AvailableKeys())
	}()

	flowTx := flow.NewTransaction().
		SetScript(script).
		SetReferenceBlockID(referenceBlockHeader.ID).
		SetComputeLimit(flowGo.DefaultMaxTransactionGasLimit)

	for _, arg := range args {
		if err := flowTx.AddArgument(arg); err != nil {
			return nil, fmt.Errorf("failed to add argument: %s, with %w", arg, err)
		}
	}

	accKey, err := t.fetchSigningAccountKey()
	if err != nil {
		return nil, err
	}

	coaAddress := t.config.COAAddress
	accountKey, err := t.client.GetAccountKeyAtLatestBlock(ctx, coaAddress, accKey.Index)
	if err != nil {
		accKey.Done()
		return nil, err
	}

	if err := accKey.SetProposerPayerAndSign(flowTx, coaAddress, accountKey); err != nil {
		accKey.Done()
		return nil, err
	}

	// now that the transaction is prepared, store the transaction's metadata
	accKey.SetLockMetadata(flowTx.ID(), referenceBlockHeader.Height)

	return flowTx, nil
}

func (t *SingleTxPool) fetchSigningAccountKey() (*keystore.AccountKey, error) {
	// getting an account key from the `KeyStore` for signing transactions,
	// should be lock-protected, so that we don't sign any two Flow
	// transactions with the same account key
	t.mux.Lock()
	defer t.mux.Unlock()

	return t.keystore.Take()
}

func (t *SingleTxPool) getReferenceBlock() *flow.BlockHeader {
	if v := t.referenceBlockHeader.Load(); v != nil {
		return v.(*flow.BlockHeader)
	}
	return nil
}

func (t *SingleTxPool) updateReferenceBlock(ctx context.Context) {
	ticker := time.NewTicker(referenceBlockUpdateFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			blockHeader, err := t.client.GetLatestBlockHeader(ctx, false)
			if err != nil {
				t.logger.Error().Err(err).Msg(
					"failed to update the reference block",
				)
				continue
			}
			t.referenceBlockHeader.Store(blockHeader)
		}
	}
}
