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
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

// BatchTxPool is a TxPool implementation that collects and groups transactions
// by EOA signer, sorts them by nonce, and submits them as a batch via
// EVM.batchRun on each flush interval.
//
// # Problem
//
// Flow does not have a traditional EVM mempool. On standard EVM chains, when a
// wallet sends transactions out-of-nonce-sequence (e.g., nonces 5, 7, 6 in
// parallel), the mempool holds future-nonce transactions until the gap is
// filled. Flow EVM has no such holding mechanism — a transaction whose nonce
// does not match the current account nonce is simply dropped.
//
// The original BatchTxPool implementation partially addressed this by batching
// transactions that arrived from an EOA with "recent activity" (i.e., a prior
// transaction within TxBatchInterval). However, it still submitted the FIRST
// transaction from any burst immediately — before the rest of the burst had
// a chance to arrive. If that first transaction happened to carry a future
// nonce (due to parallel dispatch), it failed, and the gap it left caused all
// subsequent nonces in the batch to fail as well.
//
// # Root cause (confirmed on testnet, 2026-06-03)
//
// A burst of 10 transactions was sent in parallel from a fresh wallet using
// shuffled nonce order: [8 7 3 9 2 1 6 0 4 5].
//
//   - The gateway accepted all 10 (eth_sendRawTransaction returned success).
//   - Nonce 6 arrived first → no prior EOA activity → submitted immediately
//     as a standalone Cadence transaction → failed (on-chain nonce was 0).
//   - The remaining 9 nonces [7 3 9 2 1 0 4 5 8] were pooled, sorted to
//     [0 1 2 3 4 5 7 8 9], and submitted as EVM.batchRun.
//   - EVM.batchRun executed 0→5 successfully, then encountered nonce 7.
//     The expected nonce was 6 (which had already failed), so execution
//     stopped. Nonces 7, 8, 9 were dropped.
//   - Final result: 6/10 landed on-chain; 4 were silently dropped.
//
// # Fix
//
// All transactions are now always enqueued in the pool regardless of prior EOA
// activity. The flush timer (TxBatchInterval) is the sole submission trigger.
// This guarantees that parallel transactions from the same wallet accumulate
// in the pool before being sorted by nonce and submitted atomically.
//
// Trade-off: every transaction now incurs up to TxBatchInterval of additional
// latency before it is submitted to Flow. For the mainnet default of 2.5 s this
// is acceptable; operators can lower the interval for latency-sensitive
// deployments.
type BatchTxPool struct {
	*SingleTxPool
	pooledTxs map[gethCommon.Address][]pooledEvmTx
	txMux     sync.Mutex
}

type pooledEvmTx struct {
	txPayload cadence.String
	txHash    gethCommon.Hash
	nonce     uint64
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

	batchPool := &BatchTxPool{
		SingleTxPool: singleTxPool,
		pooledTxs:    make(map[gethCommon.Address][]pooledEvmTx),
		txMux:        sync.Mutex{},
	}

	go batchPool.processPooledTransactions(ctx)

	return batchPool, nil
}

// Add enqueues the transaction in the per-EOA pool. It is never submitted
// immediately; the flush goroutine (processPooledTransactions) handles
// submission on every TxBatchInterval tick.
//
// Enqueueing everything — including the first transaction from a burst — is
// the key invariant that prevents the "first tx escapes" race described in the
// type-level comment above.
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

	userTx := pooledEvmTx{txPayload: hexEncodedTx, txHash: tx.Hash(), nonce: tx.Nonce()}
	// Prevent submission of duplicate transactions, based on their tx hash
	if slices.Contains(t.pooledTxs[from], userTx) {
		return errs.ErrDuplicateTransaction
	}
	t.pooledTxs[from] = append(t.pooledTxs[from], userTx)

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
	// Sort by nonce to guarantee correct execution order regardless of the
	// order in which transactions arrived at the gateway.
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
		txHashes := make([]string, len(pooledTxs))
		for i, tx := range pooledTxs {
			txHashes[i] = tx.txHash.Hex()
		}
		t.logger.Error().Err(err).Strs("tx-hashes", txHashes).Msg("failed to build Flow transaction, EVM transactions dropped")
		return err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		t.collector.TransactionsDropped(len(pooledTxs))
		txHashes := make([]string, len(pooledTxs))
		for i, tx := range pooledTxs {
			txHashes[i] = tx.txHash.Hex()
		}
		t.logger.Error().Err(err).Strs("tx-hashes", txHashes).Msg("failed to send Flow transaction, EVM transactions dropped")
		return err
	}

	return nil
}
