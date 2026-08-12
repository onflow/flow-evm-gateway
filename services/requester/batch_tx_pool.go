package requester

// BatchTxPool shares several building blocks with the newer TxMemPool
// (see tx_mempool.go): the NonceProvider / NonceView interfaces used to
// consult the local state index, the fastPathSubmitTimeout bound on
// synchronous submits, and the flushReason* labels used for metrics and
// logs. Those symbols are intentionally single-sourced there.

import (
	"context"
	"encoding/hex"
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

const (
	// Max number of transactions per EOA batch submission. The Cadence
	// transaction that wraps and executes EVM transactions with
	// `EVM.batchRun`, is submitted with a computation limit of `9,999`.
	// Adding more EVM transactions per batch, increases the risk of
	// running out of computation limit, which will revert all wrapped
	// EVM transactions.
	maxTxBatch = 5
	// Max nonce lookahead per EOA relative to the on-chain frontier: any
	// pooled tx whose nonce exceeds `stateNonce + maxNonceLookahead` is
	// dropped during flush pruning. Independent of queue length.
	maxNonceLookahead = 50
	// Absolute cap on the number of transactions queued per EOA, enforced
	// at admission (Add) time to bound memory even when the client uses
	// contiguous nonces within `maxNonceLookahead`.
	maxEOAQueueSize = 50
	// Multiplication factor for the submission spacing interval, which
	// gives an indication that an EOA queue is stale and could be
	// removed, to avoid unconstrained memory growth.
	stalenessFactor = 2
)

// BatchTxPool is a TxPool implementation that collects and groups transactions
// by EOA signer, sorts them by nonce, and submits them as a batch via
// EVM.batchRun on each flush interval.
//
// # Problem
//
// Flow did not have a traditional EVM mempool. On standard EVM chains, when a
// wallet sends transactions out-of-nonce-sequence (e.g., nonces 5, 7, 6 in
// parallel), the mempool holds future-nonce transactions until the gap is
// filled. Flow EVM had no such pooling mechanism — a transaction whose nonce
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
// # Fix
//
// For all incoming transactions, we are now inspecting the EOA's current nonce
// in the local state index:
// 1. If it matches the transaction nonce, we submit it right away, and record
// this activity in the EOA's dedicated queue, for use in future submissions.
// 2. If the transaction nonce is higher, we check for any recent submissions
// to see if we can form a valid sequence. This could happen from in-flight
// transaction submission, that have not yet been index by the local state
// index. In this case we optimistically submit right away. and record this
// activity in the EOA's dedicated queue, for use in future submissions.
// 3. If none of the above 2 conditions are met, we enqueue the transaction
// in the pool. The flush timer (TxBatchInterval) is the sole submission trigger.
// This guarantees that parallel transactions from the same wallet accumulate
// in the pool before being sorted by nonce and submitted atomically.
type BatchTxPool struct {
	*SingleTxPool

	nonceProvider NonceProvider
	txQueues      map[gethCommon.Address]*txQueue
	txQueuesMux   sync.Mutex
}

// txQueue tracks the pooled transactions and submission state for one EOA.
type txQueue struct {
	// txs holds pooled transactions keyed by nonce. Keying by nonce gives
	// last-write-wins semantics when a client resubmits a not-yet-sent
	// transaction with the same nonce (e.g. to change its payload).
	txs map[uint64]pooledEvmTx
	// lastSubmittedAt is when the last Cadence tx for this EOA was submitted
	// (used for submission spacing).
	lastSubmittedAt time.Time
	// lastSubmittedNonce is the last EOA nonce that was submitted with a
	// Cadence tx (used for classifying whether to queue or submit right away).
	lastSubmittedNonce uint64
}

// size returns the total number of pooled transactions.
func (t *txQueue) size() int {
	return len(t.txs)
}

// spacingElapsed checks whether enough spacing has elapsed since the EOA's
// last activity, for the given current time and spacing duration.
func (t *txQueue) spacingElapsed(now time.Time, spacing time.Duration) bool {
	return t.lastSubmittedAt.IsZero() || now.Sub(t.lastSubmittedAt) >= spacing
}

// staleEntry checks whether the recorded submission activity for the EOA
// is stale and could be removed from the mempool queue, to avoid memory
// growth. If there are no pooled transactions and the last submission was
// 2 times more than the given spacing interval, we can safely remove this
// entry.
func (t *txQueue) staleEntry(now time.Time, spacing time.Duration) bool {
	return len(t.txs) == 0 && now.Sub(t.lastSubmittedAt) >= (spacing*stalenessFactor)
}

// validNonce compares the transaction nonce with the nonce from the local
// state index and the last submission activity (if any), and returns whether
// the transaction nonce is valid for submission.
func (t *txQueue) validNonce(
	txNonce uint64,
	stateNonce uint64,
) bool {
	if txNonce == stateNonce {
		return true
	}

	// a value of 0 for `lastSubmittedNonce` is legit, if this is the EOA's
	// first transaction ever, so we use `lastSubmittedAt` to differentiate
	// between Go's zero-value.
	if txNonce == t.lastSubmittedNonce+1 && !t.lastSubmittedAt.IsZero() {
		return true
	}

	return false
}

// pruneTxs drops pooled transactions that can never be submitted from this
// queue: nonces below the on-chain frontier (already used), and nonces further
// than `maxNonceLookahead` beyond the frontier (unreachable within any
// realistic gap-fill window). The absolute queue-length cap is enforced at
// admission time in Add() — this pass only prunes; it does not size-cap.
func (t *txQueue) pruneTxs(
	address gethCommon.Address,
	stateNonce uint64,
	logger zerolog.Logger,
) {
	staleNonces := make([]uint64, 0)
	for nonce, tx := range t.txs {
		// drop any pooled transactions with nonce lower than
		// the nonce in local state index
		if tx.nonce < stateNonce {
			logger.Warn().Msgf(
				"dropped tx with nonce: %d for EOA: %s, expected state nonce: %d",
				tx.nonce,
				address,
				stateNonce,
			)
			staleNonces = append(staleNonces, nonce)
			continue
		}

		// drop txs whose nonce is further ahead than we're willing
		// to hold (nonce lookahead cap).
		if tx.nonce > maxNonceLookahead+stateNonce {
			logger.Warn().Msgf(
				"dropped tx with nonce: %d for EOA: %s, exceeds nonce lookahead of %d",
				tx.nonce,
				address,
				maxNonceLookahead,
			)
			staleNonces = append(staleNonces, nonce)
		}
	}

	// remove the transactions with stale nonces from the pooled transactions.
	for _, nonce := range staleNonces {
		delete(t.txs, nonce)
	}
}

// selectSequentialNonces returns up to `maxTxBatch` pooled transactions
// forming a sequential nonce run starting exactly at `stateNonce`, sorted
// ascending. Returns an empty slice when the transaction with stateNonce
// is absent.
//
// Since the queue is keyed by nonce, we walk forward from stateNonce with
// direct map lookups — O(k) where k = returned batch length. No sort, no
// full-map scan.
func (t *txQueue) selectSequentialNonces(stateNonce uint64) []pooledEvmTx {
	txSequence := make([]pooledEvmTx, 0, maxTxBatch)
	for i := 0; i < maxTxBatch; i++ {
		tx, ok := t.txs[stateNonce]
		if !ok {
			break
		}
		txSequence = append(txSequence, tx)
		delete(t.txs, stateNonce)
		stateNonce++
	}

	return txSequence
}

// pooledEvmTx is a transaction queued in the mempool, waiting for the
// batch interval to elapse or its nonce gap to be filled.
type pooledEvmTx struct {
	txPayload cadence.String
	txHash    gethCommon.Hash
	nonce     uint64
}

// batchSubmission is a batch selected for submission, detached from the queue so
// the network call happens outside queueMux.
type batchSubmission struct {
	from               gethCommon.Address
	txs                []pooledEvmTx
	eoaQueue           *txQueue
	lastSubmittedAt    time.Time
	lastSubmittedNonce uint64
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
	nonceProvider NonceProvider,
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
		SingleTxPool:  singleTxPool,
		nonceProvider: nonceProvider,
		txQueues:      make(map[gethCommon.Address]*txQueue),
		txQueuesMux:   sync.Mutex{},
	}

	go batchPool.processPooledTransactions(ctx)

	return batchPool, nil
}

// Add inspects the nonce of the incoming transaction, and either submits it
// right away or enqueues the transaction in the per-EOA pool, in which case
// it will be processed and submitted by the flush goroutine
// (processPooledTransactions) on every `TxBatchInterval` tick.
func (t *BatchTxPool) Add(
	ctx context.Context,
	tx *gethTypes.Transaction,
) error {
	t.txPublisher.Publish(tx) // publish pending transaction event

	// tx adding should be blocking, so we don't have races when
	// pooled transactions are being processed in the background.
	t.txQueuesMux.Lock()
	defer t.txQueuesMux.Unlock()

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

	// Reject an exact duplicate of a transaction already in the queue
	// (cheapest check; needs no index read).
	eoaQueue := t.eoaQueueEntry(from)
	existing, existsAtNonce := eoaQueue.txs[tx.Nonce()]
	if existsAtNonce && existing.txHash == tx.Hash() {
		return errs.ErrDuplicateTransaction
	}
	// Reject any transaction with a nonce that has already been submitted.
	if !eoaQueue.lastSubmittedAt.IsZero() && tx.Nonce() <= eoaQueue.lastSubmittedNonce {
		return errs.ErrInFlightNonce
	}
	// Bound per-EOA memory. A same-nonce replacement (last-write-wins) does
	// not grow the queue and is allowed even at the cap.
	if !existsAtNonce && eoaQueue.size() >= maxEOAQueueSize {
		return errs.ErrTxPoolFull
	}
	userTx := pooledEvmTx{txPayload: hexEncodedTx, txHash: tx.Hash(), nonce: tx.Nonce()}

	// get the latest nonce from the local state index
	nonce, err := t.nonceProvider.GetNextNonce(from)
	if err != nil {
		t.logger.Error().Err(err).Msgf(
			"failed to get nonce for EOA: %s", from,
		)
		return err
	}

	// Check the `txQueue` for an entry with the given EOA. If enough spacing
	// has elapsed and the tx nonce is the next expected, we submit right
	// away and update the `lastSubmittedAt` & `lastSubmittedNonce` fields,
	// for classifying future submissions, that might arrive shortly.
	if eoaQueue.spacingElapsed(time.Now(), t.config.TxBatchInterval) && eoaQueue.validNonce(tx.Nonce(), nonce) {
		// Bound the submit so a hung call cannot pin `txQueuesMux`
		// indefinitely (see `fastPathSubmitTimeout`).
		submitCtx, cancel := context.WithTimeout(ctx, fastPathSubmitTimeout)
		flowTxID, err := t.submitSingleTransaction(submitCtx, hexEncodedTx)
		cancel()
		if err != nil {
			// If there was any error during transaction submission,
			// we record it as a dropped transaction.
			t.collector.TransactionsDropped(1)
			t.logger.Error().Err(err).Str("tx_hash", tx.Hash().Hex()).Msgf(
				"failed to submit Flow transaction for EOA: %s, with nonce: %d",
				from.Hex(),
				tx.Nonce(),
			)
			return err
		}
		t.logSubmission(from, []pooledEvmTx{userTx}, flushReasonFastPath, flowTxID)

		eoaQueue.lastSubmittedAt = time.Now()
		eoaQueue.lastSubmittedNonce = tx.Nonce()
		// the submitted nonce must not stay queued, otherwise the flush
		// loop can resubmit a superseded payload for the same nonce.
		delete(eoaQueue.txs, tx.Nonce())
		t.collector.TxPoolSubmission(flushReasonFastPath)
		return nil
	}

	eoaQueue.txs[tx.Nonce()] = userTx

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
			// construct a block view here, to read the nonce for each
			// EOA below, without recreating the block view each time.
			blockView, err := t.nonceProvider.GetBlockView()
			if err != nil {
				t.logger.Error().Err(err).Msg(
					"failed to construct BlockView for nonce reading",
				)
				continue
			}

			t.txQueuesMux.Lock()
			txBatchByAddress := make(map[gethCommon.Address]batchSubmission)
			staleEntries := make([]gethCommon.Address, 0)
			queues := len(t.txQueues)
			queuedTxs := 0
			for address, eoaQueue := range t.txQueues {
				if eoaQueue.staleEntry(time.Now(), t.config.TxBatchInterval) {
					staleEntries = append(staleEntries, address)
					continue
				}
				queuedTxs += eoaQueue.size()

				// get the latest nonce from the local state index.
				nonce, err := blockView.GetNonce(address)
				if err != nil {
					t.logger.Error().Err(err).Msgf(
						"failed to get nonce for EOA: %s", address,
					)
					continue
				}

				// drop any pooled transactions with nonce lower than
				// the local state index nonce.
				eoaQueue.pruneTxs(address, nonce, t.logger)

				// pick the txs with the valid nonce sequence
				txSequence := eoaQueue.selectSequentialNonces(nonce)
				// if there is no valid nonce sequence, according to the local state
				// index, continue with the next EOA.
				if len(txSequence) == 0 {
					continue
				}
				txBatchByAddress[address] = batchSubmission{
					from:               address,
					txs:                txSequence,
					eoaQueue:           eoaQueue,
					lastSubmittedAt:    eoaQueue.lastSubmittedAt,
					lastSubmittedNonce: eoaQueue.lastSubmittedNonce,
				}
				// reserve the nonce range before releasing the lock, so a
				// concurrent `Add()` cannot fast-path the same nonces.
				eoaQueue.lastSubmittedAt = time.Now()
				eoaQueue.lastSubmittedNonce = txSequence[len(txSequence)-1].nonce
			}

			// cleanup any stale entries, to avoid unconstrained memory growth
			for _, address := range staleEntries {
				delete(t.txQueues, address)
			}
			t.txQueuesMux.Unlock()

			for address, batch := range txBatchByAddress {
				flowTxID, err := t.batchSubmitTransactionsForSameAddress(
					ctx,
					t.getReferenceBlock(),
					batch.txs,
				)
				t.txQueuesMux.Lock()
				if err != nil {
					t.logger.Error().Err(err).Msgf(
						"failed to submit batch Flow transaction for EOA: %s, batch count: %d, nonce: %d, tx hash: %s",
						address.Hex(),
						len(batch.txs),
						batch.txs[0].nonce,
						batch.txs[0].txHash.Hex(),
					)
					// In case of any submission errors, add the transactions back
					// to the pool as a retry mechanism. This is an important part
					// to avoid gaps, which would require users to resubmit.
					// Rollback the nonce range reservation from before — but only
					// if a concurrent Add() has not already advanced past it via
					// the fast path, otherwise we'd erase legitimate activity.
					eoaQueue := t.eoaEnqueueTxs(address, batch.txs)
					if eoaQueue.lastSubmittedNonce == batch.txs[len(batch.txs)-1].nonce {
						eoaQueue.lastSubmittedNonce = batch.lastSubmittedNonce
						eoaQueue.lastSubmittedAt = batch.lastSubmittedAt
					}
				} else {
					// Merge the ack with any concurrent Add() fast-path that
					// advanced the queue while we were off-lock: never regress
					// lastSubmittedNonce, and use the LATER of the two timestamps
					// for spacing accounting.
					batchTail := batch.txs[len(batch.txs)-1].nonce
					if batchTail > batch.eoaQueue.lastSubmittedNonce {
						batch.eoaQueue.lastSubmittedNonce = batchTail
					}
					now := time.Now()
					if now.After(batch.eoaQueue.lastSubmittedAt) {
						batch.eoaQueue.lastSubmittedAt = now
					}
					t.collector.TxPoolSubmission(flushReasonPrefix)
					t.logSubmission(address, batch.txs, flushReasonPrefix, flowTxID)
				}
				t.txQueuesMux.Unlock()
			}

			t.collector.TxPoolSize(queues, queuedTxs)
		}
	}
}

func (t *BatchTxPool) batchSubmitTransactionsForSameAddress(
	ctx context.Context,
	referenceBlockHeader *flow.BlockHeader,
	pooledTxs []pooledEvmTx,
) (flow.Identifier, error) {
	// the `pooledTxs` slice is already sorted by nonce, in ascending order
	// inside the `processPooledTransactions()` function.
	hexEncodedTxs := make([]cadence.Value, len(pooledTxs))
	for i, txPayload := range pooledTxs {
		hexEncodedTxs[i] = txPayload.txPayload
	}

	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return flow.Identifier{}, err
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
		txHashes := make([]string, len(pooledTxs))
		for i, tx := range pooledTxs {
			txHashes[i] = tx.txHash.Hex()
		}
		t.logger.Error().Err(err).Strs("tx_hashes", txHashes).Msg("failed to build Flow transaction, EVM transactions re-queued")
		return flow.Identifier{}, err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		txHashes := make([]string, len(pooledTxs))
		for i, tx := range pooledTxs {
			txHashes[i] = tx.txHash.Hex()
		}
		t.logger.Error().Err(err).Strs("tx_hashes", txHashes).Msg("failed to send Flow transaction, EVM transactions re-queued")
		return flow.Identifier{}, err
	}

	return flowTx.ID(), nil
}

func (t *BatchTxPool) submitSingleTransaction(
	ctx context.Context,
	hexEncodedTx cadence.String,
) (flow.Identifier, error) {
	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return flow.Identifier{}, err
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
		return flow.Identifier{}, err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		return flow.Identifier{}, err
	}

	return flowTx.ID(), nil
}

// eoaQueueEntry returns the corresponding txQueue for the given address.
// One will be created if it doesn't yet exist.
func (t *BatchTxPool) eoaQueueEntry(address gethCommon.Address) *txQueue {
	queue, ok := t.txQueues[address]
	if !ok {
		queue = &txQueue{
			txs: make(map[uint64]pooledEvmTx),
		}
		t.txQueues[address] = queue
	}
	return queue
}

// eoaEnqueueTxs re-adds the given transactions to the corresponding txQueue
// for the given address (used as a rollback path on submission failure), and
// returns the txQueue. One will be created if it doesn't yet exist. A same-
// nonce entry already in the queue is preserved: a concurrent Add() may have
// dropped a fresher payload there while we were off-lock, and last-write-wins
// for the client means the fresh payload must win over the failed batch.
func (t *BatchTxPool) eoaEnqueueTxs(address gethCommon.Address, txs []pooledEvmTx) *txQueue {
	queue, ok := t.txQueues[address]
	if !ok {
		queue = &txQueue{
			txs: make(map[uint64]pooledEvmTx),
		}
		t.txQueues[address] = queue
	}
	for _, tx := range txs {
		if _, exists := queue.txs[tx.nonce]; exists {
			continue
		}
		queue.txs[tx.nonce] = tx
	}

	return queue
}

// logSubmission records the fate of a submitted batch so a transaction is
// never silently lost.
func (t *BatchTxPool) logSubmission(
	from gethCommon.Address,
	txs []pooledEvmTx,
	reason string,
	flowTxID flow.Identifier,
) {
	if len(txs) == 0 {
		return
	}

	event := t.logger.Info().
		Str("eoa", from.Hex()).
		Uint64("low-nonce", txs[0].nonce).
		Uint64("high-nonce", txs[len(txs)-1].nonce).
		Int("batch-size", len(txs)).
		Str("reason", reason)

	if flowTxID != (flow.Identifier{}) {
		event = event.Str("flow_tx_id", flowTxID.Hex())
	}
	event.Msg("submitted EVM transactions to Flow")
}
