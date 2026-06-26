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

// TODO: Document what the TX Mempool does with clear examples of the difference use cases it handles. e.g. transactions coming in out of sequence with a gap in between, transactions expiring in the queue as local index nonce moves beyond the nonce of the the tx.
// TODO: Overall, refactor the code such that its easier to understand and more importantly easier to maintain
// TODO: Log when transactions are being discarded from the queue to help debug issues if lets say a client compains about transactions being lost or erroring out.

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

// nonceWrapper represents a nonce that may not be set: when set is true, v is a
// valid nonce; when set is false, the nonce has not been initialized yet. It
// lets us compare nonces uniformly even when one may be absent — including the
// ambiguous case where the value is 0 (a valid nonce) but set is false. An unset
// nonceWrapper behaves as -∞ in the comparisons below (atLeast/is/max): it is
// below every real nonce and never "at or above" one, so callers carry no set
// checks.
type nonceWrapper struct {
	v   uint64
	set bool
}

func toNonceWrapper(v uint64) nonceWrapper { return nonceWrapper{v: v, set: true} }

// atLeast reports whether this nonce is set and >= n. An unset nonce (-∞) is
// never >= a real nonce, so it returns false.
func (o nonceWrapper) atLeast(n uint64) bool { return o.set && o.v >= n }

// is reports whether this nonce is set and exactly equals n.
func (o nonceWrapper) is(n uint64) bool { return o.set && o.v == n }

// max returns the greater of two optional nonces, treating unset as -∞.
func (o nonceWrapper) max(other nonceWrapper) nonceWrapper {
	if !o.set {
		return other
	}
	if other.set && other.v > o.v {
		return other
	}
	return o
}

// nonceVerdict is what classify decides should happen to an incoming nonce.
type nonceVerdict int

const (
	// nonceNextExpected: nonce == expectedNonce; eligible for immediate submit.
	nonceNextExpected nonceVerdict = iota
	// nonceInFlight: nonce is at or below one we have already sent (still in
	// flight or already ack'd). Re-accepting it would burn Flow fees on a
	// guaranteed nonce-mismatch, so it is rejected.
	nonceInFlight
	// nonceTooLow: nonce is below the on-chain frontier — already used, can
	// never execute. Rejected up front.
	nonceTooLow
	// nonceTooHigh: nonce is more than maxNonceGap beyond the on-chain frontier;
	// it cannot execute until the gap fills, so it is rejected up front.
	nonceTooHigh
	// nonceQueue: a future nonce beyond the expected one (a gap ahead) but within
	// the accepted window; hold it.
	nonceQueue
)

// nonceTracker is the per-EOA submission-state machine. It records the nonce
// facts the mempool reasons about and answers "what should happen to an
// incoming nonce?" (classify) and "what is the next nonce to submit?"
// (expectedNonce), so callers never compare raw fields or write compound
// conditions. All methods assume the pool's queueMux is held: the tracker has
// no lock of its own, and the single submit goroutine plus Add-under-lock model
// guarantees serialized access (see TxMemPool docstring).
type nonceTracker struct {
	// localIndexedNonce is the EOA's next expected nonce per the local state
	// index (the on-chain frontier). A CACHE refreshed from a fresh read via
	// refreshIndexed — a fact about the chain, not about our sends.
	localIndexedNonce uint64
	// submitting is the highest nonce SENT to Flow but not yet ack'd: the window
	// between collecting a batch and its submit result. Unset when no submission
	// is outstanding. It means strictly "a network call is in flight" and is
	// cleared the moment that call returns — by markSubmitted on success or
	// rollbackSubmitting on failure.
	submitting nonceWrapper
	// lastConsecutivelySubmitted is the highest nonce CONSECUTIVELY, SUCCESSFULLY
	// submitted (ack'd). Unset before the EOA's first success. Only consecutive
	// submissions advance it; TTL-expiry (gapped) batches do NOT, so it never
	// includes a nonce past a gap.
	lastConsecutivelySubmitted nonceWrapper
	// maxNonceGap is how far above localIndexedNonce a nonce may be before it is
	// rejected as too-high. 0 means no upper bound. It does NOT affect the
	// too-low check (a nonce below localIndexedNonce is always rejected). Set
	// once from config when the queue is created.
	maxNonceGap uint64
}

// inFlight reports whether a submission is outstanding (sent, not yet ack'd).
func (n *nonceTracker) inFlight() bool { return n.submitting.set }

// highestSent returns the highest nonce we have already sent — whether still in
// flight or already ack'd (unset if neither). Re-accepting a nonce at or below
// it would burn Flow fees on a guaranteed nonce-mismatch.
func (n *nonceTracker) highestSent() nonceWrapper {
	return n.lastConsecutivelySubmitted.max(n.submitting)
}

// expectedNonce is the next nonce eligible for submission: one past the highest
// nonce already sent, or the indexed frontier, whichever is higher.
func (n *nonceTracker) expectedNonce() uint64 {
	if hi := n.highestSent(); hi.atLeast(n.localIndexedNonce) {
		return hi.v + 1
	}
	return n.localIndexedNonce
}

// classify returns the verdict for an incoming nonce, refreshing the cached
// on-chain frontier as part of the decision.
//
// A nonce at or below our highest already-sent nonce is an in-flight/duplicate
// retry, rejected from local state alone — no index read. We check this first
// precisely because reading the frontier builds a block view (the expensive
// step) that the retry case must avoid.
//
// Any other nonce is beyond what we've sent, so the remaining verdicts
// (too-low/too-high/next-expected) need the on-chain frontier: we read and
// refresh it. A read error is an exception (a local state-index read should not
// fail) and is returned so the caller can reject the transaction. Pruning of any
// now-stale queued txs is deferred to collectDueBatches.
//
// Callers must hold queueMux.
func (n *nonceTracker) classify(
	nonce uint64,
	np NonceProvider,
	from gethCommon.Address,
) (nonceVerdict, error) {
	// At or below the highest nonce we've already sent: in flight or submitted.
	if n.highestSent().atLeast(nonce) {
		return nonceInFlight, nil
	}

	// Beyond what we've sent: refresh the frontier for the remaining verdicts.
	indexNonce, err := np.GetNonce(from)
	if err != nil {
		return 0, err
	}
	n.refreshIndexed(indexNonce)

	// Below the on-chain frontier: already used, can never execute.
	if nonce < n.localIndexedNonce {
		return nonceTooLow, nil
	}
	// More than maxNonceGap beyond the frontier (only when a gap is configured):
	// cannot execute until the gap fills. A behind (stale) index can only make
	// this over-strict, which is acceptable — the gateway is catching up.
	if n.maxNonceGap > 0 && nonce > n.localIndexedNonce+n.maxNonceGap {
		return nonceTooHigh, nil
	}
	if nonce == n.expectedNonce() {
		return nonceNextExpected, nil
	}
	return nonceQueue, nil
}

// markSubmitting records that nonces up to highNonce have been sent but not yet
// ack'd. Set when a batch is detached for async submission in collectDueBatches
// (the synchronous fast path in Add skips it — it holds the lock across the
// whole submit, so there is no concurrency window to guard).
func (n *nonceTracker) markSubmitting(highNonce uint64) {
	n.submitting = toNonceWrapper(highNonce)
}

// markSubmitted acks a successful submission: it advances the consecutively-
// submitted nonce and clears the in-flight marker. Called the moment a
// submission succeeds, EXPLICITLY and under the lock, rather than waiting for
// the index to confirm — so `submitting` strictly means "a network call is
// outstanding" and is cleared as soon as the call returns (here on success, or
// via rollbackSubmitting on failure). This costs one quick lock per successful
// submission, which we accept for a state machine that is trivial to reason
// about.
func (n *nonceTracker) markSubmitted(highNonce uint64) {
	n.lastConsecutivelySubmitted = toNonceWrapper(highNonce)
	n.submitting = nonceWrapper{}
}

// rollbackSubmitting clears the in-flight marker after a FAILED submission, but
// only when it still refers to highNonce (a newer submission may have replaced
// it). Because a failure never advances lastConsecutivelySubmitted, there is
// nothing else to undo: the next flush recomputes expectedNonce from the
// unchanged frontier. This replaces the old, fragile
// "lastSubmittedNonce == batchMax" guard.
func (n *nonceTracker) rollbackSubmitting(highNonce uint64) {
	if n.submitting.is(highNonce) {
		n.submitting = nonceWrapper{}
	}
}

// refreshIndexed updates the cached on-chain frontier from a fresh index read.
func (n *nonceTracker) refreshIndexed(indexedNonce uint64) {
	n.localIndexedNonce = indexedNonce
}

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
	// lastSubmittedAt is when the last Cadence tx for this EOA was submitted
	// (used for submission spacing).
	lastSubmittedAt time.Time
	// lastActivity is when this EOA was last touched — a transaction received
	// (Add) or a batch flushed (collectDueBatches). It bounds memory: a queue
	// with no held txs and no activity past idleQueueRetention is removed.
	lastActivity time.Time
	// nonces is the submission-state machine for this EOA.
	nonces nonceTracker
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
		// A fresh queue's other fields are intentionally left at their zero
		// values: the nonceTracker's nonceWrapper fields read as "unset" (nonce 0
		// is not mistaken for a real submission), and the timing fields
		// (collectionWindowEndsAt/flushDeadline/lastSubmittedAt) are only ever
		// read after being set on the first enqueue or submission below. Only
		// maxNonceGap needs seeding from config.
		q = &eoaQueue{
			txs:    make(map[uint64]heldTx),
			nonces: nonceTracker{maxNonceGap: t.config.TxMaxNonceGap},
		}
		t.queues[from] = q
	}

	now := time.Now()
	// The EOA was "touched" even if this turns out to be a duplicate, so record
	// activity here to keep the idle-queue retention clock accurate.
	q.lastActivity = now

	userTx := heldTx{
		txPayload:  hexEncodedTx,
		txHash:     tx.Hash(),
		nonce:      tx.Nonce(),
		enqueuedAt: now,
	}

	// Reject an exact duplicate of a transaction already in the queue (cheapest
	// check; needs no index read).
	if existing, ok := q.txs[tx.Nonce()]; ok && existing.txHash == tx.Hash() {
		return errs.ErrDuplicateTransaction
	}

	// Classify the nonce, reading the on-chain frontier only when needed (see
	// classify). A read error is an exception — reject rather than routing
	// through the queue path.
	verdict, err := q.nonces.classify(tx.Nonce(), t.nonceProvider, from)
	if err != nil {
		return err
	}

	switch verdict {
	case nonceInFlight:
		return errs.ErrInFlightNonce
	case nonceTooLow:
		return errs.ErrNonceTooLow
	case nonceTooHigh:
		return errs.ErrNonceTooHigh
	case nonceNextExpected:
		// Fast path: an empty queue with nothing in flight and spacing satisfied
		// can submit the expected nonce immediately — zero added latency. The
		// lock is held across the whole submit, so there is no concurrency
		// window: no need to mark "submitting" first; on success we record the
		// ack, on failure we leave the EOA untouched. If we cannot fast-path yet
		// (queue non-empty, in flight, or spacing not elapsed), fall through to
		// enqueue and let the background loop flush it.
		if q.isEmpty() && !q.nonces.inFlight() && t.spacingElapsed(q, now) {
			if submitErr := t.submitBatch(ctx, []heldTx{userTx}); submitErr != nil {
				return submitErr
			}
			q.nonces.markSubmitted(tx.Nonce())
			q.lastSubmittedAt = time.Now()
			return nil
		}
	case nonceQueue:
		// A gap ahead within the accepted window — fall through to enqueue.
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
	// inFlight is true for consecutive-prefix batches, which mark the queue's
	// nonceTracker "submitting" and must therefore reconcile it once the submit
	// returns (markSubmitted on success, rollbackSubmitting on failure — see
	// reconcileSubmission). TTL-expiry batches are not marked in flight and must
	// never touch the tracker.
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

// submitWork submits one detached batch and reconciles the queue's nonce state
// once the network call returns.
func (t *TxMemPool) submitWork(ctx context.Context, w flushWork) error {
	err := t.submitBatch(ctx, w.txs)
	t.reconcileSubmission(w, err)
	return err
}

// reconcileSubmission updates the EOA's nonceTracker after a detached
// consecutive-prefix submission returns. collectDueBatches marked the batch
// "submitting" (under the lock) before the network call; this records the
// outcome:
//
//   - On SUCCESS we explicitly advance the consecutively-submitted nonce now
//     (markSubmitted), under the lock, rather than waiting for the index to
//     confirm. This is the deliberate "update on success" decision: it keeps
//     `submitting` meaning strictly "a network call is outstanding".
//
//   - On FAILURE the batch's transactions are dropped (already counted and
//     logged by submitBatch) and never reach the chain, so we clear the
//     "submitting" marker (rollbackSubmitting). Without this, every resubmission
//     of those nonces would be rejected as in-flight forever and the index would
//     never advance to clear the marker — the EOA would be permanently wedged.
//     rollbackSubmitting clears the marker only if it still refers to this
//     batch (a newer submission may have replaced it). lastSubmittedAt is
//     deliberately NOT restored: a brief, self-correcting spacing delay after a
//     rare failure is harmless.
//
// TTL-expiry batches (w.inFlight == false) never mark the tracker, so there is
// nothing to reconcile for them.
func (t *TxMemPool) reconcileSubmission(w flushWork, submitErr error) {
	if !w.inFlight {
		return
	}

	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	q, ok := t.queues[w.from]
	if !ok {
		return
	}

	highNonce := w.txs[len(w.txs)-1].nonce
	if submitErr != nil {
		q.nonces.rollbackSubmitting(highNonce)
		return
	}
	q.nonces.markSubmitted(highNonce)
}

// collectDueBatches selects, under the queue lock, every batch that is due
// for submission, updates the queue state optimistically, and returns the
// detached work items.
func (t *TxMemPool) collectDueBatches() []flushWork {
	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	now := time.Now()
	work := make([]flushWork, 0)

	// Each due EOA's nonce is read via GetNonce; the provider caches the block
	// view by indexed height, so all reads in this pass (and across ticks at the
	// same height) reuse one built view rather than rebuilding per address.
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

		q.nonces.refreshIndexed(indexNonce)

		// Prune transactions that can never execute: their nonce is already
		// used on-chain (e.g. filled via another gateway). They would only
		// burn fees at TTL expiry.
		t.pruneStaleTxs(q, from, indexNonce)

		// At most one batch is collected per EOA per tick. The consecutive
		// prefix below takes precedence and `continue`s; only when there is no
		// eligible prefix (a gap at the head) do we consider the TTL-expiry
		// path. The post-gap / over-cap remainder is left in the queue and
		// drained on a later tick, gated by submission spacing — it is never
		// merged with this batch, since a head gap would make the whole Flow
		// transaction fail.
		prefix := selectConsecutivePrefix(q.txs, q.nonces.expectedNonce(), t.config.TxMaxBatchSize)
		if len(prefix) > 0 {
			for _, htx := range prefix {
				delete(q.txs, htx.nonce)
			}
			// Optimistically mark the batch submitting; reconcileSubmission
			// advances submitted on success or clears it on failure.
			q.nonces.markSubmitting(prefix[len(prefix)-1].nonce)
			q.lastSubmittedAt = now
			q.lastActivity = now
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
