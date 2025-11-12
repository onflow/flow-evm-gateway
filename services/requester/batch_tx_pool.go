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
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

const eoaActivityCacheSize = 10_000

type pooledEvmTx struct {
	txPayload cadence.String
	txHash    gethCommon.Hash
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
// arrive in a short time interval (about the same as Flow's block production rate),
// will be executed in the same order their arrived.
// This helps to reduce the nonce mismatch errors which mainly occur from the
// re-ordering of Cadence transactions that happens from Collection nodes.
type BatchTxPool struct {
	*SingleTxPool
	pooledTxs   map[gethCommon.Address][]pooledEvmTx
	txMux       sync.Mutex
	eoaActivity *expirable.LRU[gethCommon.Address, time.Time]
}

var _ TxPool = &BatchTxPool{}

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

	eoaActivity := expirable.NewLRU[gethCommon.Address, time.Time](
		eoaActivityCacheSize,
		nil,
		config.EOAActivityCacheTTL,
	)
	batchPool := &BatchTxPool{
		SingleTxPool: singleTxPool,
		pooledTxs:    make(map[gethCommon.Address][]pooledEvmTx),
		txMux:        sync.Mutex{},
		eoaActivity:  eoaActivity,
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

	// tx adding should be blocking, so we don't have races when
	// pooled transactions are being processed in the background.
	t.txMux.Lock()
	defer t.txMux.Unlock()

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
	lastActivityTime, found := t.eoaActivity.Get(from)

	if !found {
		// Case 1. EOA activity not found:
		err = t.submitSingleTransaction(ctx, hexEncodedTx)
	} else if time.Since(lastActivityTime) > t.config.TxBatchInterval {
		// Case 2. EOA activity found AND it was more than [X] seconds ago:
		err = t.submitSingleTransaction(ctx, hexEncodedTx)
	} else {
		// Case 3. EOA activity found AND it was less than [X] seconds ago:
		userTx := pooledEvmTx{txPayload: hexEncodedTx, txHash: tx.Hash(), nonce: tx.Nonce()}
		// Prevent submission of duplicate transactions, based on their tx hash
		if slices.Contains(t.pooledTxs[from], userTx) {
			return errs.ErrDuplicateTransaction
		}
		t.pooledTxs[from] = append(t.pooledTxs[from], userTx)
	}

	t.eoaActivity.Add(from, time.Now())

	return err
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
						"failed to submit Flow transaction from BatchTxPool for EOA: %s",
						address.Hex(),
					)
					continue
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
		return err
	}

	return nil
}
