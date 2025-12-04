package requester

import (
	"context"
	"encoding/hex"
	"slices"
	"sort"
	"sync"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

const (
	eoaActivityCacheSize     = 10_000
	maxTrackedTxNoncesPerEOA = 30
)

type pooledEvmTx struct {
	txPayload cadence.String
	nonce     uint64
}

type eoaActivityMetadata struct {
	lastSubmission time.Time
	txNonces       []uint64
}

// BatchTxPool is a `TxPool` implementation that groups incoming transactions
// based on their EOA signer, and submits them for execution using a batch.
//
// The underlying Cadence EVM API used, is `EVM.batchRun`, instead of the
// `EVM.run` used in `SingleTxPool`.
//
// The main advantage of this implementation over the `SingleTxPool`, is the
// guarantee that transactions originating from the same EOA address, which
// arrive in a short time interval (configurable by the node operator),
// will be executed in the same order they arrived.
// This helps to reduce the execution errors which may occur from the
// re-ordering of Cadence transactions that happens on Collection nodes.
type BatchTxPool struct {
	*SingleTxPool

	pooledTxs        map[gethCommon.Address][]pooledEvmTx
	txMux            sync.Mutex
	eoaActivityCache *expirable.LRU[gethCommon.Address, eoaActivityMetadata]
}

var _ TxPool = (*BatchTxPool)(nil)

func NewBatchTxPool(
	ctx context.Context,
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
) (*BatchTxPool, error) {
	// initialize the available keys metric since it is only updated when sending a tx
	collector.AvailableSigningKeys(keystore.AvailableKeys())

	singleTxPool, err := NewSingleTxPool(
		ctx,
		client,
		transactionsPublisher,
		logger,
		config,
		collector,
		keystore,
	)
	if err != nil {
		return nil, err
	}

	eoaActivityCache := expirable.NewLRU[gethCommon.Address, eoaActivityMetadata](
		eoaActivityCacheSize,
		nil,
		config.EOAActivityCacheTTL,
	)
	batchPool := &BatchTxPool{
		SingleTxPool:     singleTxPool,
		pooledTxs:        make(map[gethCommon.Address][]pooledEvmTx),
		txMux:            sync.Mutex{},
		eoaActivityCache: eoaActivityCache,
	}

	go batchPool.processPooledTransactions(ctx)

	return batchPool, nil
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

	from, err := models.DeriveTxSender(tx)
	if err != nil {
		return err
	}

	txData, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(txData))
	if err != nil {
		return err
	}

	t.txMux.Lock()
	defer t.txMux.Unlock()

	eoaActivity, found := t.eoaActivityCache.Get(from)
	nonce := tx.Nonce()

	// Skip transactions that have been already submitted,
	// as they are *likely* to fail.
	if found && slices.Contains(eoaActivity.txNonces, nonce) {
		t.logger.Info().
			Str("evm_tx", tx.Hash().Hex()).
			Str("from", from.Hex()).
			Uint64("nonce", nonce).
			Msg("tx with same nonce has been already submitted")
		return nil
	}

	// Scenarios
	// 1. EOA activity not found:
	// => We send the transaction individually, without adding it
	// to the batch pool.
	//
	// 2. EOA activity found AND it was more than [X] seconds ago:
	// => We send the transaction individually, without adding it
	// to the batch pool.
	//
	// 3. EOA activity found AND it was less than [X] seconds ago:
	// => We add the transaction to the batch pool, so that it gets
	// processed and submitted according to the configured
	// `TxBatchInterval`.
	//
	// For all 3 cases, we record the activity time for the next
	// transactions that might come from the same EOA.
	// [X] is equal to the configured `TxBatchInterval` duration.
	if !found {
		// Case 1. EOA activity not found:
		err = t.submitSingleTransaction(ctx, hexEncodedTx)
	} else if time.Since(eoaActivity.lastSubmission) > t.config.TxBatchInterval {
		// If the EOA has pooled transactions, which are not yet processed,
		// due to congestion or anything, make sure to include the current
		// tx on that batch.
		hasBatch := len(t.pooledTxs[from]) > 0
		if hasBatch {
			userTx := pooledEvmTx{txPayload: hexEncodedTx, nonce: nonce}
			t.pooledTxs[from] = append(t.pooledTxs[from], userTx)
		}

		// If it wasn't batched, submit individually
		if !hasBatch {
			// Case 2. EOA activity found AND it was more than [X] seconds ago:
			err = t.submitSingleTransaction(ctx, hexEncodedTx)
		}
	} else {
		// Case 3. EOA activity found AND it was less than [X] seconds ago:
		userTx := pooledEvmTx{txPayload: hexEncodedTx, nonce: nonce}
		t.pooledTxs[from] = append(t.pooledTxs[from], userTx)
	}

	if err != nil {
		t.logger.Error().Err(err).Msgf(
			"failed to submit single Flow transaction for EOA: %s",
			from.Hex(),
		)
		return err
	}

	t.updateEOAActivityMetadata(from, nonce)

	return nil
}

func (t *BatchTxPool) processPooledTransactions(ctx context.Context) {
	ticker := time.NewTicker(t.config.TxBatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Take a copy here to allow `Add()` to continue accept
			// incoming EVM transactions, without blocking until the
			// batch transactions are submitted.
			t.txMux.Lock()
			txsGroupedByAddress := t.pooledTxs
			t.pooledTxs = make(map[gethCommon.Address][]pooledEvmTx)
			t.txMux.Unlock()

			for address, pooledTxs := range txsGroupedByAddress {
				err := t.batchSubmitTransactionsForSameAddress(
					ctx,
					t.getReferenceBlock(),
					pooledTxs,
				)
				if err != nil {
					t.logger.Error().Err(err).Msgf(
						"failed to submit batch Flow transaction for EOA: %s",
						address.Hex(),
					)
					// In case of any error, add the transactions back to the pool,
					// as a retry mechanism.
					t.txMux.Lock()
					t.pooledTxs[address] = append(t.pooledTxs[address], pooledTxs...)
					t.txMux.Unlock()
				}
			}
		}
	}
}

func (t *BatchTxPool) batchSubmitTransactionsForSameAddress(
	ctx context.Context,
	referenceBlockHeader *flow.BlockHeader,
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

	script := replaceAddresses(runTxScript, t.config.FlowNetworkID)
	flowTx, err := t.buildTransaction(
		ctx,
		referenceBlockHeader,
		script,
		cadence.NewArray(hexEncodedTxs),
		coinbaseAddress,
	)
	if err != nil {
		// If there was any error during the transaction build
		// process, we record all transactions as dropped.
		t.collector.TransactionsDropped(len(hexEncodedTxs))
		return err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		// If there was any error while sending the transaction,
		// we record all transactions as dropped.
		t.collector.TransactionsDropped(len(hexEncodedTxs))
		return err
	}

	return nil
}

func (t *BatchTxPool) submitSingleTransaction(
	ctx context.Context,
	hexEncodedTx cadence.String,
) error {
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
		// If there was any error while sending the transaction,
		// we record it as a dropped transaction.
		t.collector.TransactionsDropped(1)
		return err
	}

	return nil
}

func (t *BatchTxPool) updateEOAActivityMetadata(
	from gethCommon.Address,
	nonce uint64,
) {
	// Update metadata for the last EOA activity only on successful add/submit.
	eoaActivity, _ := t.eoaActivityCache.Get(from)
	eoaActivity.lastSubmission = time.Now()
	eoaActivity.txNonces = append(eoaActivity.txNonces, nonce)
	// To avoid the slice of nonces from growing indefinitely,
	// keep only the last `maxTrackedTxNoncesPerEOA` nonces.
	if len(eoaActivity.txNonces) > maxTrackedTxNoncesPerEOA {
		firstKeep := len(eoaActivity.txNonces) - maxTrackedTxNoncesPerEOA
		eoaActivity.txNonces = eoaActivity.txNonces[firstKeep:]
	}

	t.eoaActivityCache.Add(from, eoaActivity)
}
