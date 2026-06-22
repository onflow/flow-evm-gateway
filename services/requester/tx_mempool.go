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

// heldTx is a transaction held in the mempool, waiting for its
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

// txMemPoolTickInterval is the resolution at which due queues are
// scanned and flushed. Deadlines are therefore honored with up to this
// much slack, which is acceptable relative to the 300ms collection window.
const txMemPoolTickInterval = 50 * time.Millisecond

// idleQueueRetention is how long a queue with no held transactions and no
// recent activity is kept before being removed, to bound memory usage.
const idleQueueRetention = time.Minute

// eoaQueue tracks the held transactions and submission state for one EOA.
type eoaQueue struct {
	// txs holds pending transactions keyed by nonce. Keying by nonce gives
	// last-write-wins semantics when a client resubmits a not-yet-sent
	// transaction with the same nonce (e.g. to change its payload).
	txs map[uint64]heldTx
	// collectionWindowEndsAt is lastArrival + TxCollectionWindow. The queue is
	// not flushed until the current time has passed this instant: it marks the
	// end of the sliding collection window, NOT a deadline to act before.
	collectionWindowEndsAt time.Time
	// flushDeadline is firstEnqueue + TxSubmissionSpacing. It caps how long
	// a continuously-resetting collection window can defer a flush. There is
	// deliberately no separate "hard cap" knob: TxSubmissionSpacing serves
	// both purposes (see PR #965 discussion).
	flushDeadline time.Time
	// lastSubmittedAt is when the last Cadence tx for this EOA was submitted.
	lastSubmittedAt time.Time
	// lastSubmittedNonce is the highest nonce included in the last submission.
	// Only meaningful while hasInFlight is true. (Kept next to lastSubmittedAt:
	// "submitted" and "sent" mean the same action here.)
	lastSubmittedNonce uint64
	// lastActivity is when this EOA was last touched — a transaction received
	// (Add) or a batch flushed (collectDueBatches). It bounds memory: a queue
	// with no held txs and no activity past idleQueueRetention is removed.
	lastActivity time.Time
	// hasInFlight reports whether a submission exists that the local index
	// has not yet confirmed (index nonce <= lastSubmittedNonce).
	hasInFlight bool
}

// isEmpty reports whether the queue holds no transactions. Callers must hold
// the pool's queueMux.
func (q *eoaQueue) isEmpty() bool {
	return len(q.txs) == 0
}

// size returns the number of transactions held in the queue. Callers must hold
// the pool's queueMux.
func (q *eoaQueue) size() int {
	return len(q.txs)
}

// TxMemPool is a `TxPool` implementation that uses the EOA nonce from
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
//
// Note on locking: fast-path submissions hold the pool-wide queue lock for
// the duration of one Flow submission, trading cross-EOA throughput for the
// simplicity of atomic state updates; a per-EOA lock is the known upgrade
// path if contention shows up.
type TxMemPool struct {
	*SingleTxPool
	nonceProvider NonceProvider
	queues        map[gethCommon.Address]*eoaQueue
	queueMux      sync.Mutex
	// submitBatch performs the actual Flow submission. It defaults to
	// submitTxBatch and exists as a field so tests can inject a fake.
	submitBatch func(ctx context.Context, txs []heldTx) error
}

var _ TxPool = &TxMemPool{}

func NewTxMemPool(
	ctx context.Context,
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
	nonceProvider NonceProvider,
) (*TxMemPool, error) {
	singleTxPool, err := NewSingleTxPool(
		ctx, client, transactionsPublisher, logger, config, collector, keystore,
	)
	if err != nil {
		return nil, err
	}

	pool := &TxMemPool{
		SingleTxPool:  singleTxPool,
		nonceProvider: nonceProvider,
		queues:        make(map[gethCommon.Address]*eoaQueue),
	}
	pool.submitBatch = pool.submitTxBatch

	go pool.processQueues(ctx)

	return pool, nil
}

// Add submits the transaction immediately when it carries the expected next
// nonce and nothing is queued or in flight for the EOA; otherwise it
// enqueues the transaction for the background flush loop.
func (t *TxMemPool) Add(
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
	q.lastActivity = now

	userTx := heldTx{
		txPayload:  hexEncodedTx,
		txHash:     tx.Hash(),
		nonce:      tx.Nonce(),
		enqueuedAt: now,
	}

	// Reject obvious cases before reading the index nonce — each read builds a
	// full block view, so when we already know we will reject the transaction
	// we must not pay that cost.

	// Reject an exact duplicate of a transaction already in the queue.
	if existing, ok := q.txs[tx.Nonce()]; ok && existing.txHash == tx.Hash() {
		return errs.ErrDuplicateTransaction
	}

	// Reject a nonce that has been submitted and is still in flight: it
	// would burn Flow fees on a guaranteed nonce-mismatch failure. A nonce at
	// or below lastSubmittedNonce while hasInFlight is inherently in flight, so
	// rejecting it here is correct.
	//
	// Note (zhangchiqing): ErrInFlightNonce covers a nonce at/below the last
	// in-flight nonce. A nonce strictly below the indexed (already-used) nonce
	// is NOT separately distinguished here — doing so would require an extra
	// index read on every Add. Such a transaction is instead pruned by
	// pruneStaleTxs on the background loop, or fails observably on-chain.
	if q.hasInFlight && tx.Nonce() <= q.lastSubmittedNonce {
		return errs.ErrInFlightNonce
	}

	// Read the index nonce — an expensive operation that builds a full block
	// view — at most once per Add, and only when it can change the decision:
	// to clear a stale in-flight marker, and/or to evaluate the fast path for
	// an empty, spacing-satisfied queue.
	if q.hasInFlight || (q.isEmpty() && t.spacingElapsed(q, now)) {
		indexNonce, nonceErr := q.queryAndRefreshInFlight(t.nonceProvider, from)
		if nonceErr != nil {
			// A nonce lookup failure is an exception, not an expected
			// condition: this is a local state-index read that should not
			// fail under normal operation. The gateway is in an unknown
			// state, so reject the transaction rather than silently routing
			// it through the queue path.
			return nonceErr
		}

		// We deliberately do NOT prune stale txs (nonce < indexNonce) here,
		// even though the index may have just advanced: pruning is deferred to
		// collectDueBatches, which walks every queued tx anyway, so repeating
		// it per-Add would be redundant work.

		// Fast path: the queue is empty, nothing is in flight (the marker may
		// have just been cleared above), spacing is satisfied and this tx is
		// exactly the next expected nonce. Submit right away — zero added
		// latency for the common case.
		if q.isEmpty() && !q.hasInFlight &&
			t.spacingElapsed(q, now) && tx.Nonce() == indexNonce {
			if submitErr := t.submitBatch(ctx, []heldTx{userTx}); submitErr != nil {
				// Submission failed: leave queue state untouched so the EOA
				// is neither marked in flight nor rate-limited behind a tx
				// that never landed.
				return submitErr
			}
			q.lastSubmittedAt = time.Now()
			q.lastSubmittedNonce = tx.Nonce()
			q.hasInFlight = true
			return nil
		}
		// On an unexpected nonce, fall through to the queue path.
	}

	// Enqueue. A same-nonce, different-payload resubmission replaces the
	// queued transaction (last write wins), matching mempool semantics.
	wasEmpty := q.isEmpty()
	q.txs[tx.Nonce()] = userTx
	q.collectionWindowEndsAt = now.Add(t.config.TxCollectionWindow)
	// Anchor the flush deadline at the FIRST enqueue only. Re-arming it on a
	// same-nonce replacement would let a client defer the flush indefinitely
	// by resubmitting one held transaction before each deadline.
	if wasEmpty {
		q.flushDeadline = now.Add(t.config.TxSubmissionSpacing)
	}

	return nil
}

// refreshInFlight clears the in-flight marker once the local index has
// advanced past the last submitted nonce. Callers must hold the pool's
// queueMux.
func (q *eoaQueue) refreshInFlight(indexNonce uint64) {
	if q.hasInFlight && indexNonce > q.lastSubmittedNonce {
		q.hasInFlight = false
	}
}

// queryAndRefreshInFlight reads the EOA's current nonce from the local index
// and clears the in-flight marker if the index has advanced past the last
// submitted nonce, returning the index nonce. Callers must hold queueMux.
func (q *eoaQueue) queryAndRefreshInFlight(
	np NonceProvider,
	from gethCommon.Address,
) (uint64, error) {
	indexNonce, err := np.GetNonce(from)
	if err != nil {
		return 0, err
	}
	q.refreshInFlight(indexNonce)
	return indexNonce, nil
}

// spacingElapsed reports whether enough time has passed since the last
// Cadence submission for this EOA. Callers must hold queueMux.
func (t *TxMemPool) spacingElapsed(q *eoaQueue, now time.Time) bool {
	return q.lastSubmittedAt.IsZero() ||
		now.Sub(q.lastSubmittedAt) >= t.config.TxSubmissionSpacing
}

// flushWork is a batch selected for submission, detached from the queue so
// the network call happens outside queueMux.
type flushWork struct {
	from gethCommon.Address
	txs  []heldTx
	// inFlight is true for consecutive-prefix batches, which optimistically
	// advance lastSubmittedNonce/hasInFlight on the queue and must therefore
	// roll those back if the submission fails (see rollbackFailedSubmission).
	// TTL-expiry batches are not marked in flight and must never clear the
	// marker.
	inFlight bool
}

func (t *TxMemPool) processQueues(ctx context.Context) {
	ticker := time.NewTicker(txMemPoolTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, w := range t.collectDueBatches() {
				if err := t.submitWork(ctx, w); err != nil {
					t.logger.Error().Err(err).Msgf(
						"failed to submit Flow transaction from TxMemPool for EOA: %s",
						w.from.Hex(),
					)
				}
			}
		}
	}
}

// submitWork submits one detached batch. On failure it rolls back the state
// the queue committed optimistically when the batch was collected; on success
// there is nothing to do — that optimistic state already reflects the
// submission.
func (t *TxMemPool) submitWork(ctx context.Context, w flushWork) error {
	err := t.submitBatch(ctx, w.txs)
	if err != nil {
		t.rollbackFailedSubmission(w)
	}
	return err
}

// rollbackFailedSubmission re-opens an EOA after a failed flush.
// collectDueBatches optimistically marks a consecutive-prefix batch in flight
// and advances lastSubmittedNonce BEFORE the network call. If that call fails,
// the batch's transactions are dropped (already counted and logged by
// submitBatch) and never reach the chain — so without this rollback every
// resubmission of those nonces would be rejected with ErrInFlightNonce, and the
// index would never advance past them to clear the marker: the EOA would be
// permanently wedged.
//
// The marker is cleared only when it still belongs to the failed batch — a
// newer submission may have replaced it while the failed one was on the wire.
// Only in-flight (prefix) batches are rolled back; TTL-expiry batches never set
// the marker.
//
// lastSubmittedAt is deliberately NOT restored: a brief, self-correcting
// spacing delay after a rare submission failure is harmless and not worth the
// extra bookkeeping.
func (t *TxMemPool) rollbackFailedSubmission(w flushWork) {
	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	q, ok := t.queues[w.from]
	if !ok {
		return
	}
	if w.inFlight && q.hasInFlight && q.lastSubmittedNonce == w.txs[len(w.txs)-1].nonce {
		q.hasInFlight = false
	}
}

// collectDueBatches selects, under the queue lock, every batch that is due
// for submission, updates the queue state optimistically, and returns the
// detached work items.
func (t *TxMemPool) collectDueBatches() []flushWork {
	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	now := time.Now()
	work := make([]flushWork, 0)

	for from, q := range t.queues {
		if q.isEmpty() {
			// Bound memory: drop queues with no held txs and no activity past
			// the retention period. Any in-flight submission has long since
			// resolved on-chain after this window, so discarding a lingering
			// in-flight marker here is safe — a later transaction for the EOA
			// creates a fresh queue and re-reads the index nonce.
			if now.Sub(q.lastActivity) > idleQueueRetention {
				delete(t.queues, from)
			}
			continue
		}

		// Not due yet: both the sliding window and the flush deadline are
		// still in the future.
		if now.Before(q.collectionWindowEndsAt) && now.Before(q.flushDeadline) {
			continue
		}

		// Safety gap since the previous submission not yet elapsed.
		if !t.spacingElapsed(q, now) {
			continue
		}

		indexNonce, err := t.nonceProvider.GetNonce(from)
		if err != nil {
			// Exception: a local state-index nonce read should not fail
			// under normal operation. This is a background loop with no
			// caller to reject the tx to, so skip this EOA for the current
			// tick (its batch is deferred until the read succeeds) without
			// aborting the whole flush for other EOAs.
			t.logger.Error().Err(err).Str("eoa", from.Hex()).
				Msg("unexpected failure reading nonce from local index, skipping EOA this tick")
			continue
		}

		q.refreshInFlight(indexNonce)

		// Prune transactions that can never execute: their nonce is already
		// used on-chain (e.g. filled via another gateway). They would only
		// burn fees at TTL expiry.
		t.pruneStaleTxs(q, from, indexNonce)

		expected := indexNonce
		if q.hasInFlight && q.lastSubmittedNonce+1 > expected {
			expected = q.lastSubmittedNonce + 1
		}

		// At most one batch is collected per EOA per tick. The consecutive
		// prefix below takes precedence and `continue`s; only when there is no
		// eligible prefix (a gap at the head) do we consider the TTL-expiry
		// path. The post-gap / over-cap remainder is left in the queue and
		// drained on a later tick, gated by submission spacing — it is never
		// merged with this batch, since a head gap would make the whole Flow
		// transaction fail.
		prefix := selectConsecutivePrefix(q.txs, expected, t.config.TxMaxBatchSize)
		if len(prefix) > 0 {
			for _, htx := range prefix {
				delete(q.txs, htx.nonce)
			}
			q.lastSubmittedNonce = prefix[len(prefix)-1].nonce
			q.lastSubmittedAt = now
			q.lastActivity = now
			q.hasInFlight = true
			if !q.isEmpty() {
				// Re-arm for the remaining (post-gap or over-cap) txs.
				q.collectionWindowEndsAt = now.Add(t.config.TxCollectionWindow)
				q.flushDeadline = now.Add(t.config.TxSubmissionSpacing)
			}
			work = append(work, flushWork{
				from:     from,
				txs:      prefix,
				inFlight: true,
			})
			continue
		}

		// No eligible prefix (gap at the head). Submit transactions held
		// past their TTL anyway instead of dropping them.
		//
		// Rationale (no silent drops): submitting an unexecutable transaction
		// produces a real, observable on-chain failure (operators can see the
		// failed Flow transaction and its nonce-mismatch), whereas silently
		// dropping it leaves no trace. The no-silent-drop requirement is the
		// whole reason this pool exists (see PR #965 / DFNS), so an observable
		// failure is strictly preferable to an invisible drop.
		//
		// Known tradeoff (zhangchiqing): a transaction whose nonce is far
		// ahead of the index will still be submitted at TTL and burn fees on a
		// guaranteed failure. Rejecting far-ahead nonces in Add is deliberately
		// NOT done — it remains an open design question (how far ahead is "too
		// far", and whether to add a knob for it), and is intentionally left
		// out of this change rather than guessed at.
		//
		// Cap the batch at TxMaxBatchSize so a long-lived gap cannot produce an
		// unbounded Flow transaction; the remainder drains on later ticks,
		// gated by submission spacing.
		expired := selectExpired(q.txs, now, t.config.TxPoolTTL)
		if len(expired) > t.config.TxMaxBatchSize {
			expired = expired[:t.config.TxMaxBatchSize]
		}
		if len(expired) > 0 {
			for _, htx := range expired {
				delete(q.txs, htx.nonce)
			}
			// Deliberately do NOT set hasInFlight/lastSubmittedNonce here: these
			// nonces are out of order, and marking them in flight would
			// corrupt the expected-nonce computation for future flushes.
			q.lastSubmittedAt = now
			q.lastActivity = now
			txHashes := make([]string, len(expired))
			for i, htx := range expired {
				txHashes[i] = htx.txHash.Hex()
			}
			t.logger.Warn().Strs("tx-hashes", txHashes).Str("eoa", from.Hex()).
				Msg("nonce gap never filled within TTL, submitting held transactions anyway")
			work = append(work, flushWork{
				from: from,
				txs:  expired,
			})
		}
	}

	// Report the pool's memory footprint while still holding queueMux: the
	// number of per-EOA queues and the total number of held transactions.
	// Counting must happen under the lock since t.queues is mutated
	// concurrently (and idle queues are pruned above).
	queuedTxs := 0
	for _, q := range t.queues {
		queuedTxs += q.size()
	}
	t.collector.TxPoolSize(len(t.queues), queuedTxs)

	return work
}

// pruneStaleTxs removes queued transactions whose nonce is below the current
// index nonce. They are guaranteed to fail with nonce-too-low and would only
// burn fees. Callers must hold queueMux.
func (t *TxMemPool) pruneStaleTxs(
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
		t.logger.Warn().Strs("tx-hashes", stale).Str("eoa", from.Hex()).
			Msg("dropping stale transactions with nonce below indexed state")
	}
}

// submitTxBatch wraps the given (nonce-ascending) transactions in a single
// Cadence transaction and sends it to the Flow network. The run.cdc script
// uses EVM.run for a single tx and EVM.batchRun for multiple.
func (t *TxMemPool) submitTxBatch(ctx context.Context, txs []heldTx) error {
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

func (t *TxMemPool) logTxsDropped(txs []heldTx, err error, msg string) {
	txHashes := make([]string, len(txs))
	for i, htx := range txs {
		txHashes[i] = htx.txHash.Hex()
	}
	t.logger.Error().Err(err).Strs("tx-hashes", txHashes).Msg(msg)
}
