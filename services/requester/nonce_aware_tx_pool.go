package requester

import (
	"context"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

// heldTx is a transaction held in the nonce-aware pool, waiting for its
// collection window to elapse or its nonce gap to be filled.
type heldTx struct {
	txPayload  cadence.String
	txHash     gethCommon.Hash
	nonce      uint64
	enqueuedAt time.Time
}

// selectConsecutivePrefix returns up to maxBatch held transactions forming a
// consecutive nonce run starting exactly at expectedNonce, sorted ascending.
// Returns an empty slice when the transaction with expectedNonce is absent.
func selectConsecutivePrefix(
	txs map[uint64]heldTx,
	expectedNonce uint64,
	maxBatch int,
) []heldTx {
	prefix := make([]heldTx, 0)
	for nonce := expectedNonce; len(prefix) < maxBatch; nonce++ {
		tx, ok := txs[nonce]
		if !ok {
			break
		}
		prefix = append(prefix, tx)
	}
	return prefix
}

// selectExpired returns the held transactions older than ttl, sorted by
// nonce ascending.
func selectExpired(
	txs map[uint64]heldTx,
	now time.Time,
	ttl time.Duration,
) []heldTx {
	expired := make([]heldTx, 0)
	for _, tx := range txs {
		if now.Sub(tx.enqueuedAt) > ttl {
			expired = append(expired, tx)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		return expired[i].nonce < expired[j].nonce
	})
	return expired
}

// nonceAwarePoolTickInterval is the resolution at which due queues are
// scanned and flushed. Deadlines are therefore honored with up to this
// much slack, which is acceptable relative to the 300ms collection window.
const nonceAwarePoolTickInterval = 50 * time.Millisecond

// idleQueueRetention is how long an empty queue with no in-flight
// submission is kept before being removed, to bound memory usage.
const idleQueueRetention = time.Minute

// eoaQueue tracks the held transactions and submission state for one EOA.
type eoaQueue struct {
	// txs holds pending transactions keyed by nonce. Keying by nonce gives
	// last-write-wins semantics when a client resubmits a not-yet-sent
	// transaction with the same nonce (e.g. to change its payload).
	txs map[uint64]heldTx
	// windowDeadline is lastArrival + TxCollectionWindow.
	windowDeadline time.Time
	// flushDeadline is firstEnqueue + TxSubmissionSpacing. It caps how long
	// a continuously-resetting collection window can defer a flush. There is
	// deliberately no separate "hard cap" knob: TxSubmissionSpacing serves
	// both purposes (see PR #965 discussion).
	flushDeadline time.Time
	// lastSubmittedAt is when the last Cadence tx for this EOA was sent.
	lastSubmittedAt time.Time
	// lastSentNonce is the highest nonce included in the last submission.
	// Only meaningful while hasInFlight is true.
	lastSentNonce uint64
	// hasInFlight reports whether a submission exists that the local index
	// has not yet confirmed (index nonce <= lastSentNonce).
	hasInFlight bool
}

// NonceAwareTxPool is a `TxPool` implementation that uses the EOA nonce from
// the local state index to decide when and how to submit transactions to the
// Flow network.
//
// Fast path: a transaction carrying the expected next nonce, with an empty
// queue, nothing in flight, and submission spacing satisfied, is submitted
// IMMEDIATELY — zero added latency for the common case.
//
// Otherwise transactions queue per-EOA. A sliding collection window
// (`TxCollectionWindow`, reset on each arrival) decides when a burst is
// complete. `TxSubmissionSpacing` is BOTH (a) the minimum gap between
// consecutive Cadence submissions for the same EOA (so two Flow transactions
// land in different blocks and cannot be reordered by Collection Nodes) and
// (b) the flush deadline anchored at first enqueue (caps a
// continuously-resetting window). There is deliberately NO separate hard-cap
// knob.
//
// On flush, the longest consecutive nonce prefix starting at the expected
// nonce (from the local index, advanced past any in-flight submission) is
// submitted, capped at `TxMaxBatchSize`.
//
// Out-of-order transactions are held until the gap fills, the local index
// advances past them (then they are stale and pruned), or `TxPoolTTL`
// expires — on expiry they are submitted anyway so the failure is observable
// on-chain rather than a silent drop.
//
// A nonce already submitted and still in flight is rejected with
// `ErrInFlightNonce`, since a duplicate would burn Flow fees on a guaranteed
// nonce-mismatch failure.
type NonceAwareTxPool struct {
	*SingleTxPool
	nonceProvider NonceProvider
	queues        map[gethCommon.Address]*eoaQueue
	queueMux      sync.Mutex
}

var _ TxPool = &NonceAwareTxPool{}

func NewNonceAwareTxPool(
	ctx context.Context,
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
	nonceProvider NonceProvider,
) (*NonceAwareTxPool, error) {
	singleTxPool, err := NewSingleTxPool(
		ctx, client, transactionsPublisher, logger, config, collector, keystore,
	)
	if err != nil {
		return nil, err
	}

	pool := &NonceAwareTxPool{
		SingleTxPool:  singleTxPool,
		nonceProvider: nonceProvider,
		queues:        make(map[gethCommon.Address]*eoaQueue),
	}

	go pool.processQueues(ctx)

	return pool, nil
}

// Add submits the transaction immediately when it carries the expected next
// nonce and nothing is queued or in flight for the EOA; otherwise it
// enqueues the transaction for the background flush loop.
func (t *NonceAwareTxPool) Add(
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

	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	q, ok := t.queues[from]
	if !ok {
		q = &eoaQueue{txs: make(map[uint64]heldTx)}
		t.queues[from] = q
	}

	now := time.Now()
	t.refreshInFlight(q, from)

	userTx := heldTx{
		txPayload:  hexEncodedTx,
		txHash:     tx.Hash(),
		nonce:      tx.Nonce(),
		enqueuedAt: now,
	}

	// Fast path: expected nonce, empty queue, nothing in flight, spacing
	// satisfied. Submit right away — zero added latency for the common case.
	if len(q.txs) == 0 && !q.hasInFlight && t.spacingElapsed(q, now) {
		expected, nonceErr := t.nonceProvider.GetNonce(from)
		if nonceErr == nil && tx.Nonce() == expected {
			q.lastSubmittedAt = now
			if submitErr := t.submitTxBatch(ctx, []heldTx{userTx}); submitErr != nil {
				return submitErr
			}
			q.lastSentNonce = tx.Nonce()
			q.hasInFlight = true
			return nil
		}
		// On a nonce lookup error or an unexpected nonce, fall through to
		// the queue path.
	}

	// Reject an exact duplicate of a transaction already in the queue.
	if existing, ok := q.txs[tx.Nonce()]; ok && existing.txHash == tx.Hash() {
		return errs.ErrDuplicateTransaction
	}

	// Reject a nonce that has been submitted and is still in flight: it
	// would burn Flow fees on a guaranteed nonce-mismatch failure.
	if q.hasInFlight && tx.Nonce() <= q.lastSentNonce {
		return errs.ErrInFlightNonce
	}

	// Enqueue. A same-nonce, different-payload resubmission replaces the
	// queued transaction (last write wins), matching mempool semantics.
	q.txs[tx.Nonce()] = userTx
	q.windowDeadline = now.Add(t.config.TxCollectionWindow)
	if len(q.txs) == 1 {
		q.flushDeadline = now.Add(t.config.TxSubmissionSpacing)
	}

	return nil
}

// refreshInFlight clears the in-flight marker once the local index has
// advanced past the last submitted nonce. Callers must hold queueMux.
func (t *NonceAwareTxPool) refreshInFlight(q *eoaQueue, from gethCommon.Address) {
	if !q.hasInFlight {
		return
	}
	indexNonce, err := t.nonceProvider.GetNonce(from)
	if err != nil {
		return
	}
	if indexNonce > q.lastSentNonce {
		q.hasInFlight = false
	}
}

// spacingElapsed reports whether enough time has passed since the last
// Cadence submission for this EOA. Callers must hold queueMux.
func (t *NonceAwareTxPool) spacingElapsed(q *eoaQueue, now time.Time) bool {
	return q.lastSubmittedAt.IsZero() ||
		now.Sub(q.lastSubmittedAt) >= t.config.TxSubmissionSpacing
}

// flushWork is a batch selected for submission, detached from the queue so
// the network call happens outside queueMux.
type flushWork struct {
	from gethCommon.Address
	txs  []heldTx
}

func (t *NonceAwareTxPool) processQueues(ctx context.Context) {
	ticker := time.NewTicker(nonceAwarePoolTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, w := range t.collectDueBatches() {
				if err := t.submitTxBatch(ctx, w.txs); err != nil {
					t.logger.Error().Err(err).Msgf(
						"failed to submit Flow transaction from NonceAwareTxPool for EOA: %s",
						w.from.Hex(),
					)
				}
			}
		}
	}
}

// collectDueBatches selects, under the queue lock, every batch that is due
// for submission, updates the queue state optimistically, and returns the
// detached work items.
func (t *NonceAwareTxPool) collectDueBatches() []flushWork {
	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	now := time.Now()
	work := make([]flushWork, 0)

	for from, q := range t.queues {
		if len(q.txs) == 0 {
			// Bound memory: drop queues idle past the retention period.
			if !q.hasInFlight && !q.lastSubmittedAt.IsZero() &&
				now.Sub(q.lastSubmittedAt) > idleQueueRetention {
				delete(t.queues, from)
			}
			continue
		}

		// Not due yet: both the sliding window and the flush deadline are
		// still in the future.
		if now.Before(q.windowDeadline) && now.Before(q.flushDeadline) {
			continue
		}

		// Safety gap since the previous submission not yet elapsed.
		if !t.spacingElapsed(q, now) {
			continue
		}

		t.refreshInFlight(q, from)

		indexNonce, err := t.nonceProvider.GetNonce(from)
		if err != nil {
			t.logger.Warn().Err(err).Str("eoa", from.Hex()).
				Msg("failed to read nonce from local index, deferring flush")
			continue
		}

		// Prune transactions that can never execute: their nonce is already
		// used on-chain (e.g. filled via another gateway). They would only
		// burn fees at TTL expiry.
		t.pruneStaleTxs(q, from, indexNonce)

		expected := indexNonce
		if q.hasInFlight && q.lastSentNonce+1 > expected {
			expected = q.lastSentNonce + 1
		}

		prefix := selectConsecutivePrefix(q.txs, expected, t.config.TxMaxBatchSize)
		if len(prefix) > 0 {
			for _, htx := range prefix {
				delete(q.txs, htx.nonce)
			}
			q.lastSentNonce = prefix[len(prefix)-1].nonce
			q.lastSubmittedAt = now
			q.hasInFlight = true
			if len(q.txs) > 0 {
				// Re-arm for the remaining (post-gap or over-cap) txs.
				q.windowDeadline = now.Add(t.config.TxCollectionWindow)
				q.flushDeadline = now.Add(t.config.TxSubmissionSpacing)
			}
			work = append(work, flushWork{from: from, txs: prefix})
			continue
		}

		// No eligible prefix (gap at the head). Submit transactions held
		// past their TTL anyway: they will fail on-chain with a real error,
		// which is observable, instead of being silently dropped.
		expired := selectExpired(q.txs, now, t.config.TxPoolTTL)
		if len(expired) > 0 {
			for _, htx := range expired {
				delete(q.txs, htx.nonce)
			}
			q.lastSubmittedAt = now
			txHashes := make([]string, len(expired))
			for i, htx := range expired {
				txHashes[i] = htx.txHash.Hex()
			}
			t.logger.Warn().Strs("tx-hashes", txHashes).Str("eoa", from.Hex()).
				Msg("nonce gap never filled within TTL, submitting held transactions anyway")
			work = append(work, flushWork{from: from, txs: expired})
		}
	}

	return work
}

// pruneStaleTxs removes queued transactions whose nonce is below the current
// index nonce. They are guaranteed to fail with nonce-too-low and would only
// burn fees. Callers must hold queueMux.
func (t *NonceAwareTxPool) pruneStaleTxs(
	q *eoaQueue,
	from gethCommon.Address,
	indexNonce uint64,
) {
	stale := make([]string, 0)
	for nonce, htx := range q.txs {
		if nonce < indexNonce {
			stale = append(stale, htx.txHash.Hex())
			delete(q.txs, nonce)
		}
	}
	if len(stale) > 0 {
		t.collector.TransactionsDropped(len(stale))
		t.logger.Warn().Strs("tx-hashes", stale).Str("eoa", from.Hex()).
			Msg("dropping stale transactions with nonce below indexed state")
	}
}

// submitTxBatch wraps the given (nonce-ascending) transactions in a single
// Cadence transaction and sends it to the Flow network. The run.cdc script
// uses EVM.run for a single tx and EVM.batchRun for multiple.
func (t *NonceAwareTxPool) submitTxBatch(ctx context.Context, txs []heldTx) error {
	hexEncodedTxs := make([]cadence.Value, len(txs))
	for i, htx := range txs {
		hexEncodedTxs[i] = htx.txPayload
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
		cadence.NewArray(hexEncodedTxs),
		coinbaseAddress,
	)
	if err != nil {
		t.collector.TransactionsDropped(len(txs))
		t.logTxsDropped(txs, err, "failed to build Flow transaction, EVM transactions dropped")
		return err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		t.collector.TransactionsDropped(len(txs))
		t.logTxsDropped(txs, err, "failed to send Flow transaction, EVM transactions dropped")
		return err
	}

	return nil
}

func (t *NonceAwareTxPool) logTxsDropped(txs []heldTx, err error, msg string) {
	txHashes := make([]string, len(txs))
	for i, htx := range txs {
		txHashes[i] = htx.txHash.Hex()
	}
	t.logger.Error().Err(err).Strs("tx-hashes", txHashes).Msg(msg)
}
