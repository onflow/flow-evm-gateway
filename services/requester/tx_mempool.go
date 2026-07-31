package requester

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
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

// This file implements TxMemPool, a nonce-aware transaction mempool that
// decides when and how to submit EVM transactions to Flow. It treats the EOA's
// nonce from the local state index as the on-chain frontier and reasons about
// each incoming nonce relative to that frontier and to what it has already sent.
//
// BEHAVIOR SPEC — every case the pool handles, written as a checklist that maps
// to code and to tests.
//
// (The examples below assume the EOA's next expected nonce is N unless stated.)
//
// Submission paths
//   1. Fast path: a transaction whose nonce is the next expected nonce, arriving
//      to an empty queue with nothing in flight and submission spacing
//      satisfied, is submitted synchronously inside Add — zero added latency.
//      Example: expected nonce 5, an empty queue, tx with nonce 5 arrives → it
//      is sent immediately, nothing is queued.
//   2. Burst batching: when the fast path does not apply, transactions queue
//      per-EOA. A sliding collection window (TxCollectionWindow, reset on each
//      arrival) decides when a burst is complete; a flush deadline anchored at
//      the FIRST enqueue (TxSubmissionSpacing) caps how long a continuously
//      resetting window can defer the flush.
//      Example: nonce 5 fast-paths and is sent; nonces 6,7,8 then arrive a few
//      ms apart while the submission-spacing gap since that send is still open,
//      so they cannot fast-path and queue instead. The window keeps resetting as
//      they arrive, and they flush together as one batch once arrivals pause for
//      TxCollectionWindow (or the deadline is hit) AND spacing has elapsed. (A
//      burst whose lead nonce is itself the next expected one on an empty, idle
//      queue would fast-path that first tx — case 1 — not queue it.)
//   3. Consecutive-prefix flush: on flush, the longest run of consecutive nonces
//      starting at the expected nonce is sent as ONE Cadence transaction, capped
//      at TxMaxBatchSize. A nonce gap splits the queue: only the prefix before
//      the first gap is sent; the post-gap remainder waits for a later tick.
//      Example: txs with nonces 1,2,3,5,6,7 arrive (gap at 4); the queue holds
//      1,2,3,5,6,7; the flush sends a batch of 1,2,3 while 5,6,7 wait in the
//      queue for nonce 4 to arrive (or to age out via case 7).
//   4. Submission spacing: consecutive Cadence submissions for one EOA are kept
//      at least TxSubmissionSpacing apart, so two Flow transactions land in
//      different blocks and cannot be reordered by Collection Nodes.
//      Example: a batch is sent at t=0; the next batch for the same EOA is held
//      until t=TxSubmissionSpacing even if it is already due.
//
// Holding and eventual disposal of out-of-order transactions
//   5. Queue (gap ahead): a future nonce within the accepted window is held
//      until the gap fills or it ages out.
//      Example: expected nonce 5, tx with nonce 7 arrives (5 and 6 missing) →
//      7 is held, not sent.
//   6. Stale pruning: a held tx whose nonce has fallen below the on-chain
//      frontier (e.g. filled via another gateway) can never execute and is
//      dropped with a WARN log before it would burn fees.
//      Example: nonce 3 sits in the queue; the frontier advances to 5 (3 and 4
//      were filled elsewhere) → 3 is dropped with a WARN, never submitted.
//   7. TTL submit-anyway: a held tx whose head gap never fills within TxPoolTTL
//      is submitted ANYWAY (capped at TxMaxBatchSize) rather than dropped, so
//      its failure is observable on-chain instead of a silent disappearance.
//      Example: nonce 7 is held while 5,6 never arrive; after TxPoolTTL, 7 is
//      submitted and fails on-chain with a nonce mismatch (visible on flowscan).
//   8. Idle-queue retention: a queue with no held txs and no activity for
//      idleQueueRetention is removed to bound memory.
//
// Rejections (returned synchronously from Add; the tx is never queued)
//   9.  Duplicate: same nonce AND same tx hash as one already queued →
//       ErrDuplicateTransaction (cheapest check; no index read).
//       Example: tx with nonce 5 is queued; the identical tx (same hash) is
//       submitted again → rejected. (A same-nonce tx with a DIFFERENT hash is
//       not a duplicate — it replaces the queued one, last write wins.)
//   10. In-flight: nonce at or below the highest nonce already sent (still in
//       flight or already ack'd) → ErrInFlightNonce; re-accepting would burn
//       Flow fees on a guaranteed nonce-mismatch.
//       Example: nonces 5,6 were just sent and are in flight; a new tx with
//       nonce 6 arrives → rejected.
//   11. Too-low: nonce below the on-chain frontier (already used) →
//       ErrNonceTooLow. Always enforced.
//       Example: frontier is 5, tx with nonce 4 arrives → rejected.
//   12. Too-high: nonce more than TxMaxNonceGap beyond the frontier →
//       ErrNonceTooHigh. Only enforced when TxMaxNonceGap > 0.
//       Example: frontier 5, TxMaxNonceGap 500, tx with nonce 600 arrives →
//       rejected (it cannot execute until ~595 intervening nonces are filled).
//
// Recovery
//   13. Reconciliation: a background loop (reconcileLoop) ticks every
//       TxReconcileInterval (default 1s). For each EOA with an outstanding
//       submission marker (highestSent() set and lastFlowTxID != zero) it
//       calls GetTransactionResult(lastFlowTxID) and resets the marker in
//       two cases:
//         (a) the wrapper is SEALED with a non-nil Error — the wrapper
//             reverted. Canonical case: two consecutive-nonce wrappers land
//             in the same Flow block, the collector executes them out of
//             order, and the higher-nonce wrapper's run.cdc assertion trips
//             with "nonce too high". No EVM.TransactionExecuted event fires
//             for the reverted wrapper, so without reconciliation the pool
//             would never learn.
//         (b) the wrapper is not sealed and now - lastSubmittedAt exceeds
//             TxReconcileStaleAfter (default 30s) — probable silent drop or
//             any never-lands case.
//       The reset clears lastConsecutivelySubmitted, submitting, and
//       lastFlowTxID so the next Add() re-classifies against on-chain state.
//       Concurrency: the (eoa, flowTxID, lastSubmittedAt) snapshot is taken
//       under queueMux, the GetTransactionResult call runs OUTSIDE the lock,
//       and the reset re-acquires the lock and (i) re-checks lastFlowTxID
//       still matches and (ii) requires q.nonces.inFlight() to be false — so
//       neither a superseding ack'd submission nor a freshly-in-flight one
//       is ever clobbered.
//       Observability: each reset emits a WARN log line with eoa,
//       flow_tx_id and reason ("wrapper-reverted" | "unsealed-past-threshold")
//       and increments the TxPoolReconcileReset counter. Wedge duration is
//       bounded to ~one sealing window (~6-8s) instead of the full
//       idleQueueRetention (60s).
//
// Cross-cutting invariants
//   - No silent drops: for any accepted tx id you can either find it on-chain
//     (submitted) or find a WARN log saying it was dropped (submit failure or
//     stale prune) — never nothing. See logSubmission. The one exception is
//     shutdown: held-but-not-yet-submitted txs are discarded without a WARN when
//     the pool's context is cancelled (no graceful drain — see processQueues).
//   - Failure handling: a failed submission drops the batch (clients resubmit)
//     and never wedges the EOA (the in-flight marker is rolled back). The pool
//     does NOT retry internally.
//   - Concurrency: one background goroutine (processQueues) flushes due queues
//     and Add runs under the same pool-wide queueMux; a second goroutine
//     (reconcileLoop) polls Cadence tx status outside the lock and only
//     acquires it briefly to reset stuck markers. See the note on TxMemPool.

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
	prefix := make([]heldTx, 0, maxBatch)
	for nonce := expectedNonce; len(prefix) < maxBatch; nonce++ {
		tx, ok := txs[nonce]
		if !ok {
			break
		}
		prefix = append(prefix, tx)
	}
	return prefix
}

// deleteByNonce removes every transaction in batch from txs, keyed by nonce.
func deleteByNonce(txs map[uint64]heldTx, batch []heldTx) {
	for _, htx := range batch {
		delete(txs, htx.nonce)
	}
}

// txHashHexes returns the hex tx hashes of a batch, for structured log fields.
func txHashHexes(txs []heldTx) []string {
	hashes := make([]string, len(txs))
	for i, htx := range txs {
		hashes[i] = htx.txHash.Hex()
	}
	return hashes
}

// selectExpired returns the held transactions older than ttl, sorted by
// nonce ascending.
func selectExpired(
	txs map[uint64]heldTx,
	now time.Time,
	ttl time.Duration,
) []heldTx {
	expired := make([]heldTx, 0, len(txs))
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
// much slack, which is acceptable relative to the collection window.
const txMemPoolTickInterval = 50 * time.Millisecond

// fastPathSubmitTimeout bounds how long a single fast-path submission may take.
// The fast path holds the pool-wide queueMux across the whole Flow submission
// (see the note on TxMemPool), so without a bound a hung Access-node call would
// block every other EOA's Add and the background flush loop for as long as the
// caller's context allows — up to RpcRequestTimeout (120s by default). This is a
// liveness safety ceiling, NOT a latency SLA: normal submits complete well
// within it and release the lock immediately; only a genuinely stalled call is
// cut off (and its tx is dropped-and-logged for the client to resubmit).
const fastPathSubmitTimeout = 10 * time.Second

// idleQueueRetention is how long a queue with no held transactions and no
// recent activity is kept before being removed, to bound memory usage.
const idleQueueRetention = time.Minute

// defaultTxReconcileInterval and defaultTxReconcileStaleAfter are the fallback
// values used by NewTxMemPool when the config leaves them at zero. This
// protects programmatic callers (e.g. e2e tests constructing a Config directly)
// from the time.NewTicker(0) panic and from a zero staleness threshold that
// would treat every unsealed tx as instantly stale. The CLI-flag defaults in
// cmd/run/cmd.go match these — the double-source-of-truth is intentional so
// both flag-driven and Go-driven constructors behave sanely.
const (
	defaultTxReconcileInterval   = time.Second
	defaultTxReconcileStaleAfter = 30 * time.Second
)

// nonceWrapper is a nonce that may be unset (set == false). It disambiguates the
// otherwise ambiguous value 0, which is both a valid nonce and the zero value.
// An unset nonceWrapper behaves as -∞ in the comparisons below (atLeast/is/max):
// below every real nonce and never "at or above" one, so callers need no set
// checks.
type nonceWrapper struct {
	v   uint64
	set bool
}

func toNonceWrapper(v uint64) nonceWrapper { return nonceWrapper{v: v, set: true} }

// atLeast reports whether this nonce is set and >= n. An unset nonce (-∞) is
// never >= a real nonce, so it returns false.
func (w nonceWrapper) atLeast(n uint64) bool { return w.set && w.v >= n }

// is reports whether this nonce is set and exactly equals n.
func (w nonceWrapper) is(n uint64) bool { return w.set && w.v == n }

// max returns the greater of two optional nonces, treating unset as -∞.
func (w nonceWrapper) max(other nonceWrapper) nonceWrapper {
	if !w.set {
		return other
	}
	if other.set && other.v > w.v {
		return other
	}
	return w
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

// normalizeNonceGap maps the configured max nonce gap to the value stored in a
// nonceTracker. The config value 0 ("no upper bound") becomes math.MaxUint64, so
// classify can use a single distance comparison without a per-call "gap > 0"
// guard. Any non-zero value is used as-is.
func normalizeNonceGap(configGap uint64) uint64 {
	if configGap == 0 {
		return math.MaxUint64
	}
	return configGap
}

// nonceTracker is the per-EOA submission-state machine. It records the nonce
// facts the mempool reasons about and answers "what should happen to an
// incoming nonce?" (classify) and "what is the next nonce to submit?"
// (expectedNonce), so callers never compare raw fields directly.
//
// The tracker has no lock of its own: all methods assume the pool's queueMux is
// held. Serialized access is guaranteed by the single submit goroutine plus
// Add running under that same lock (see the TxMemPool docstring).
type nonceTracker struct {
	// localNextNonce is the EOA's next expected nonce per the local state
	// index (the on-chain frontier). A cache of a fact about the chain, refreshed
	// from a fresh read via refreshNextNonce — not a record of our own sends.
	localNextNonce uint64
	// submitting is the highest consecutive nonce sent to Flow but not yet ack'd. It marks the
	// window between detaching a batch and its submit result returning, and
	// strictly means "a network call is in flight": it is cleared the moment that
	// call returns, by markSubmitted on success or rollbackSubmitting on failure.
	// Unset when no submission is outstanding.
	// it is also used to filter incoming txs to accept only whose nonce is bigger than this,
	// otherwise would be rejected
	submitting nonceWrapper
	// lastConsecutivelySubmitted is the highest nonce consecutively and
	// successfully submitted (ack'd). Only consecutive-prefix batches advance it;
	// TTL-expiry (gapped) batches do NOT, so it never includes a nonce past a gap.
	// Unset before the EOA's first success.
	lastConsecutivelySubmitted nonceWrapper
	// maxNonceGap is how far above localNextNonce a nonce may be before it is
	// rejected as too-high. math.MaxUint64 means no upper bound: the config value
	// TxMaxNonceGap, where 0 = "unbounded", is normalized to it at construction
	// (see normalizeNonceGap) so classify needs no per-call "is a gap configured?"
	// guard. It does NOT affect the too-low check (a nonce below localNextNonce
	// is always rejected). Set once when the queue is created.
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
// nonce already sent, or the on-chain frontier, whichever is higher.
func (n *nonceTracker) expectedNonce() uint64 {
	if hi := n.highestSent(); hi.atLeast(n.localNextNonce) {
		return hi.v + 1
	}
	return n.localNextNonce
}

// classify returns the verdict for an incoming nonce, refreshing the cached
// on-chain frontier as part of the decision.
//
// A nonce at or below our highest already-sent nonce is an in-flight/duplicate
// retry, decided from local state alone with no index read. This case is checked
// FIRST because reading the frontier builds a block view (the expensive step),
// which the retry path must avoid.
//
// Any other nonce is beyond what we've sent, so the remaining verdicts
// (too-low/too-high/next-expected) need the frontier, which is read and
// refreshed. A read error is an exception (a local state-index read should not
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
	nextNonce, err := np.GetNextNonce(from)
	if err != nil {
		return 0, fmt.Errorf("reading next nonce for %s: %w", from.Hex(), err)
	}
	n.refreshNextNonce(nextNonce)

	// Below the on-chain frontier: already used, can never execute.
	if nonce < n.localNextNonce {
		return nonceTooLow, nil
	}
	// More than maxNonceGap beyond the frontier: cannot execute until the gap
	// fills. A behind (stale) index can only make this over-strict, which is
	// acceptable — the gateway is catching up. maxNonceGap is normalized
	// (math.MaxUint64 = unbounded), so no "is a gap configured?" guard is needed.
	// We compare the distance rather than localNextNonce+maxNonceGap (which
	// could overflow): the too-low check above guarantees nonce >= localNextNonce
	// so the subtraction never underflows, and the distance is at most MaxUint64
	// so the unbounded case never trips.
	if nonce-n.localNextNonce > n.maxNonceGap {
		return nonceTooHigh, nil
	}
	if nonce == n.expectedNonce() {
		return nonceNextExpected, nil
	}
	return nonceQueue, nil
}

// markSubmitting records that nonces up to highNonce have been sent but not yet
// ack'd. Called when a batch is detached for async submission in
// collectDueBatches. The synchronous fast path in Add skips it: Add holds the
// lock across the whole submit, so there is no concurrency window to guard.
func (n *nonceTracker) markSubmitting(highNonce uint64) {
	n.submitting = toNonceWrapper(highNonce)
}

// clearSubmittingIf retracts the in-flight marker, but only if it still refers
// to highNonce (a newer submission may have replaced it once submissions run
// concurrently). Shared by the success (markSubmitted) and failure
// (rollbackSubmitting) acks — both fire when the network call returns.
func (n *nonceTracker) clearSubmittingIf(highNonce uint64) {
	if n.submitting.is(highNonce) {
		n.submitting = nonceWrapper{}
	}
}

// markSubmitted acks a successful submission: it advances the consecutively-
// submitted nonce and clears the in-flight marker. It runs the moment the
// submission succeeds, under the lock, rather than waiting for the index to
// confirm — so `submitting` strictly means "a network call is outstanding" and
// clears as soon as the call returns. The cost is one quick lock per successful
// submission, accepted in exchange for a state machine that is easy to reason
// about.
//
// Both updates are guarded so a stale ack cannot corrupt newer state:
// lastConsecutivelySubmitted only advances (never regresses), and submitting is
// cleared only if it still refers to this batch.
func (n *nonceTracker) markSubmitted(highNonce uint64) {
	n.lastConsecutivelySubmitted = n.lastConsecutivelySubmitted.max(toNonceWrapper(highNonce))
	n.clearSubmittingIf(highNonce)
}

// rollbackSubmitting clears the in-flight marker after a failed submission.
// Because a failure never advances lastConsecutivelySubmitted, there is nothing
// else to undo: the next flush recomputes expectedNonce from the unchanged
// frontier.
func (n *nonceTracker) rollbackSubmitting(highNonce uint64) {
	n.clearSubmittingIf(highNonce)
}

// refreshNextNonce updates the cached on-chain frontier from a fresh index read.
func (n *nonceTracker) refreshNextNonce(nextNonce uint64) {
	n.localNextNonce = nextNonce
}

// resetSubmissionState clears both submission markers so subsequent Add() calls
// re-classify against on-chain state. Called by the reconciliation loop when
// the wrapping Cadence tx demonstrably did not advance the on-chain nonce
// (reverted, or unsealed past the stale-after threshold). The corresponding
// eoaQueue's lastFlowTxID must be cleared alongside this call.
func (n *nonceTracker) resetSubmissionState() {
	n.lastConsecutivelySubmitted = nonceWrapper{}
	n.submitting = nonceWrapper{}
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
	// flushDeadline is firstEnqueue + TxSubmissionSpacing. It caps how long a
	// continuously-resetting collection window can defer a flush. There is
	// deliberately no separate "hard cap" knob: TxSubmissionSpacing serves both
	// purposes.
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
	// lastFlowTxID is the Flow transaction ID of the most recent Cadence submission
	// for this EOA. Zero until the first successful submission. Read by the
	// reconciliation loop to poll the wrapper's on-chain status; rolled back to
	// zero when reconciliation detects the wrapper reverted or never sealed.
	lastFlowTxID flow.Identifier
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

// TxMemPool is a `TxPool` implementation that uses the EOA nonce from the local
// state index to decide when and how to submit transactions to the Flow
// network. The full behavior — fast path, burst batching, gap handling, TTL
// submit-anyway, and the rejection rules — is enumerated in the behavior spec
// at the top of this file.
//
// `TxSubmissionSpacing` deliberately serves two roles at once: (a) the minimum
// gap between consecutive Cadence submissions for one EOA (so two Flow
// transactions land in different blocks and cannot be reordered by Collection
// Nodes) and (b) the flush deadline anchored at first enqueue. There is
// intentionally NO separate hard-cap knob.
//
// Note on locking: fast-path submissions hold the pool-wide queue lock for the
// duration of one Flow submission, trading cross-EOA throughput for the
// simplicity of atomic state updates; a per-EOA lock is the known upgrade path
// if contention shows up.
type TxMemPool struct {
	*SingleTxPool
	nonceProvider NonceProvider
	queues        map[gethCommon.Address]*eoaQueue
	queueMux      sync.Mutex
	// submitBatch exists as a field so tests can inject a fake. It returns the
	// ID of the wrapping Cadence transaction (zero on early build failure
	// before a Flow tx is signed) so logSubmission can record it, letting an
	// operator correlate a wedged EVM nonce to the specific Cadence tx.
	submitBatch func(ctx context.Context, txs []heldTx) (flow.Identifier, error)
	// getTxResult retrieves the sealed status of a Cadence transaction. It defaults
	// to t.client.GetTransactionResult and exists as a field so tests can inject a
	// fake without a live Access Node.
	getTxResult func(ctx context.Context, id flow.Identifier) (*flow.TransactionResult, error)
	// now returns the current time. It defaults to time.Now and exists as a
	// field so tests can drive the collection window, flush deadline, submission
	// spacing, TTL expiry and idle-queue retention with a controllable clock
	// rather than wall-clock sleeps.
	now func() time.Time
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
		now:           time.Now,
	}
	pool.submitBatch = pool.submitTxBatch
	pool.getTxResult = pool.client.GetTransactionResult

	// Backfill reconcile-loop knobs when a programmatic caller leaves them at
	// zero. Also protects against time.NewTicker(0) which panics.
	if pool.config.TxReconcileInterval <= 0 {
		pool.config.TxReconcileInterval = defaultTxReconcileInterval
	}
	if pool.config.TxReconcileStaleAfter <= 0 {
		pool.config.TxReconcileStaleAfter = defaultTxReconcileStaleAfter
	}

	go pool.processQueues(ctx)
	go pool.reconcileLoop(ctx)

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
		// values, each safe to read as-is:
		//   - the nonceTracker's nonceWrapper fields read as "unset", so nonce 0
		//     is never mistaken for a real submission (see nonceWrapper);
		//   - collectionWindowEndsAt and flushDeadline are read only once the
		//     queue is non-empty (collectDueBatches skips empty queues), and the
		//     first enqueue below sets them before that — their zero value is
		//     never observed;
		//   - lastSubmittedAt MAY be read while still zero (the fast path checks
		//     spacing immediately after creation), but spacingElapsed treats zero
		//     as "never submitted" → spacing satisfied, which is exactly right;
		//   - lastActivity is set unconditionally on the next line.
		// Only maxNonceGap needs seeding from config.
		q = &eoaQueue{
			txs:    make(map[uint64]heldTx),
			nonces: nonceTracker{maxNonceGap: normalizeNonceGap(t.config.TxMaxNonceGap)},
		}
		t.queues[from] = q
	}

	now := t.now()
	// The EOA was "touched" even if this turns out to be a duplicate, so record
	// activity here to keep the idle-queue retention clock accurate.
	q.lastActivity = now

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

	// Handle every verdict. The rejections return up front, before allocating any
	// held state (a transaction we are about to reject must not cost a heldTx).
	// The two keep verdicts build the heldTx and either fast-path or enqueue it.
	// An unhandled verdict is a programming error, so panic.
	switch verdict {
	case nonceInFlight:
		return errs.ErrInFlightNonce
	case nonceTooLow:
		return errs.ErrNonceTooLow
	case nonceTooHigh:
		return errs.ErrNonceTooHigh
	case nonceQueue:
		// A gap ahead within the accepted window — hold it for the background loop.
		held := heldTx{
			txPayload:  hexEncodedTx,
			txHash:     tx.Hash(),
			nonce:      tx.Nonce(),
			enqueuedAt: now,
		}
		t.enqueue(q, held, now)
		return nil
	case nonceNextExpected:
		held := heldTx{
			txPayload:  hexEncodedTx,
			txHash:     tx.Hash(),
			nonce:      tx.Nonce(),
			enqueuedAt: now,
		}
		// Fast path: an empty queue with nothing in flight and spacing satisfied
		// submits immediately — zero added latency. The lock is held across the
		// whole submit, so there is no concurrency window: no need to mark
		// "submitting" first; on success we record the ack, on failure we leave
		// the EOA untouched. Otherwise fall back to enqueue for the background loop.
		if q.isEmpty() && !q.nonces.inFlight() && t.spacingElapsed(q, now) {
			batch := []heldTx{held}
			// Bound the submit so a hung call cannot pin queueMux indefinitely
			// (see fastPathSubmitTimeout).
			submitCtx, cancel := context.WithTimeout(ctx, fastPathSubmitTimeout)
			flowTxID, submitErr := t.submitBatch(submitCtx, batch)
			cancel()
			t.logSubmission(from, batch, flushReasonFastPath, q.nonces.localNextNonce, flowTxID, submitErr)
			if submitErr != nil {
				return submitErr
			}
			q.nonces.markSubmitted(tx.Nonce())
			q.lastSubmittedAt = t.now()
			q.lastFlowTxID = flowTxID
			return nil
		}
		t.enqueue(q, held, now)
		return nil
	default:
		panic(fmt.Sprintf("unhandled nonce verdict: %d", verdict))
	}
}

// enqueue holds a transaction for the background flush loop, (re)arming the
// collection window. A same-nonce, different-payload resubmission replaces the
// queued transaction (last write wins), matching mempool semantics. Callers must
// hold queueMux.
func (t *TxMemPool) enqueue(q *eoaQueue, held heldTx, now time.Time) {
	wasEmpty := q.isEmpty()
	q.txs[held.nonce] = held
	q.collectionWindowEndsAt = now.Add(t.config.TxCollectionWindow)
	// Anchor the flush deadline at the FIRST enqueue only. Re-arming it on a
	// same-nonce replacement would let a client defer the flush indefinitely by
	// resubmitting one held transaction before each deadline.
	if wasEmpty {
		q.flushDeadline = now.Add(t.config.TxSubmissionSpacing)
	}
}

// spacingElapsed reports whether enough time has passed since the last Cadence
// submission for this EOA. Callers must hold queueMux.
//
// Timing caveat: for background batches, lastSubmittedAt is stamped at COLLECTION
// time (collectPrefix/collectExpired), not when the detached submit actually
// returns. Since one tick's batches are submitted sequentially afterward, a
// later EOA's stamp can predate its real submit by the cumulative submit latency
// ahead of it, so its NEXT batch's effective spacing can be short by that skew.
// The skew is bounded by (concurrently-due EOAs ahead) x (per-submit latency),
// which is small for the current few-EOA deployment and accepted. The
// collection-time stamp is also load-bearing the other way: it is what gates a
// follow-up batch from being collected while the previous one is still in flight
// (the spacing check runs before prefix selection, and the in-flight marker does
// not by itself stop the next consecutive prefix from being selected). A precise
// fix would keep this gate and additionally re-stamp on submit completion in
// reconcileSubmission.
func (t *TxMemPool) spacingElapsed(q *eoaQueue, now time.Time) bool {
	return q.lastSubmittedAt.IsZero() ||
		now.Sub(q.lastSubmittedAt) >= t.config.TxSubmissionSpacing
}

// flushWork is a batch selected for submission, detached from the queue so
// the network call happens outside queueMux.
type flushWork struct {
	from gethCommon.Address
	txs  []heldTx
	// needsReconcile is true when reconcileSubmission must update the tracker for
	// this batch. Consecutive-prefix batches set it: they mark the nonceTracker
	// "submitting" and must reconcile it once the submit returns (markSubmitted on
	// success, rollbackSubmitting on failure — see reconcileSubmission). TTL-expiry
	// batches leave it false: they never mark the tracker, so there is nothing to
	// reconcile.
	needsReconcile bool
	// reason is why this batch was flushed, recorded purely for the submission
	// log (see logSubmission): flushReasonPrefix for a consecutive-prefix flush,
	// flushReasonTTL for a TTL-expiry submit-anyway.
	reason string
	// localNextNonce is the on-chain next expected nonce observed when the batch was
	// collected. It is logged on drop so a "lost transaction" report can be
	// debugged against where the chain actually was.
	localNextNonce uint64
}

// flushReason values label why a batch was submitted, for the submission log.
const (
	flushReasonFastPath = "fast-path"
	flushReasonPrefix   = "consecutive-prefix"
	flushReasonTTL      = "ttl-expiry"
)

func (t *TxMemPool) processQueues(ctx context.Context) {
	ticker := time.NewTicker(txMemPoolTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Shutdown is NOT a graceful drain: any transactions still held in a
			// queue (waiting on their collection window, gap fill, or TTL) are
			// dropped without a WARN, which is the one gap in the no-silent-drops
			// invariant — it holds during steady-state operation, not across a
			// shutdown/restart. This is acceptable because clients resubmit, and
			// a held tx has not been sent to Flow (nothing on-chain to reconcile
			// against). If this ever needs to change, drain due+held batches here
			// before returning rather than relying on resubmission.
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

// submitWork submits one detached batch, records its fate (logSubmission), and
// reconciles the queue's nonce state once the network call returns.
func (t *TxMemPool) submitWork(ctx context.Context, w flushWork) error {
	flowTxID, err := t.submitBatch(ctx, w.txs)
	t.logSubmission(w.from, w.txs, w.reason, w.localNextNonce, flowTxID, err)
	t.reconcileSubmission(w, flowTxID, err)
	return err
}

// batchLogFields attaches to e the structured fields common to every per-batch
// lifecycle log — submission failure, stale prune, and TTL submit-anyway — so
// the no-silent-drops fields stay consistent and greppable across all three
// sites (eoa, tx hashes, nonce range, batch size, the on-chain frontier). The
// caller creates e at the desired level, adds any site-specific fields (reason,
// error, expected-nonce, ...), and calls Msg. The nonce range is computed as the
// true min/max so it is correct even when txs is not sorted (e.g. a stale-prune
// batch collected in map order).
func batchLogFields(
	e *zerolog.Event,
	from gethCommon.Address,
	txs []heldTx,
	nextNonce uint64,
) *zerolog.Event {
	var lowNonce, highNonce uint64
	for i, htx := range txs {
		if i == 0 || htx.nonce < lowNonce {
			lowNonce = htx.nonce
		}
		if i == 0 || htx.nonce > highNonce {
			highNonce = htx.nonce
		}
	}
	return e.
		Str("eoa", from.Hex()).
		Strs("tx-hashes", txHashHexes(txs)).
		Uint64("low-nonce", lowNonce).
		Uint64("high-nonce", highNonce).
		Int("batch-size", len(txs)).
		Uint64("local-next-nonce", nextNonce)
}

// logSubmission records the fate of a submitted batch so a transaction is never
// silently lost. This is the observability half of the no-silent-drops
// invariant: for any tx id you can either find it on-chain (sent) OR find a WARN
// log here (dropped) — never nothing.
//
//   - On a Flow submit FAILURE the batch's EVM transactions are dropped (we do
//     not retry — clients resubmit), so we WARN with the full batch context
//     (batchLogFields) plus the flush reason, the wrapping Cadence tx ID (only
//     when non-zero — omitted if the failure happened before the tx was built),
//     and the error.
//   - On SUCCESS we emit a single INFO line carrying eoa, nonce range,
//     batch-size, reason, and the wrapping Cadence tx ID — the fields an
//     operator needs to correlate an EVM nonce back to the Flow tx on
//     Flowscan.
//
// The `flow_tx_id` field is emitted only when non-zero, so an early build
// failure produces a clean log without a bogus all-zero identifier.
//
// txs is assumed nonce-ascending (selectConsecutivePrefix / selectExpired and
// the single-tx fast path all satisfy this), so txs[0] is the low nonce.
func (t *TxMemPool) logSubmission(
	from gethCommon.Address,
	txs []heldTx,
	reason string,
	localNextNonce uint64,
	flowTxID flow.Identifier,
	submitErr error,
) {
	if len(txs) == 0 {
		return
	}

	// Count every submitted batch by flush reason (fast-path / consecutive-prefix
	// / ttl-expiry), regardless of outcome; failures are also tracked by
	// TransactionsDropped.
	t.collector.TxPoolSubmission(reason)

	if submitErr != nil {
		event := batchLogFields(t.logger.Warn(), from, txs, localNextNonce).
			Str("reason", reason).
			Err(submitErr)
		if flowTxID != (flow.Identifier{}) {
			event = event.Str("flow_tx_id", flowTxID.Hex())
		}
		event.Msg("Flow submission failed, EVM transactions dropped")
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

// reconcileSubmission updates the EOA's nonceTracker after a detached
// consecutive-prefix submission returns. collectDueBatches marked the batch
// "submitting" (under the lock) before the network call; this records the
// outcome:
//
//   - On SUCCESS it advances the consecutively-submitted nonce now
//     (markSubmitted), under the lock, rather than waiting for the index to
//     confirm — keeping `submitting` meaning strictly "a network call is
//     outstanding".
//
//   - On FAILURE the batch's transactions are dropped (already counted and
//     logged) and never reach the chain, so the "submitting" marker is cleared
//     (rollbackSubmitting). Without this, resubmissions of those nonces would be
//     rejected as in-flight forever — the index never advances to clear the
//     marker, so the EOA would be permanently wedged. rollbackSubmitting clears
//     the marker only if it still refers to this batch (a newer submission may
//     have replaced it). lastSubmittedAt is deliberately NOT restored: a brief,
//     self-correcting spacing delay after a rare failure is harmless.
//
// TTL-expiry batches (w.needsReconcile == false) never mark the tracker, so
// there is nothing to reconcile for them.
//
// flowTxID is recorded on the queue when submission succeeds so the
// reconciliation loop can later poll the wrapping Cadence tx status against the
// chain (see reconcileLoop). It is ignored on failure and for batches that
// don't require reconciliation.
func (t *TxMemPool) reconcileSubmission(w flushWork, flowTxID flow.Identifier, submitErr error) {
	if !w.needsReconcile {
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
	q.lastFlowTxID = flowTxID
}

// collectDueBatches selects, under the queue lock, every batch that is due for
// submission, updates the queue state optimistically, and returns the detached
// work items. The per-EOA decision lives in collectQueue.
//
// Each due EOA's nonce is read via GetNextNonce; the provider caches the block view
// by indexed height, so all reads in this pass (and across ticks at the same
// height) reuse one built view rather than rebuilding per address.
func (t *TxMemPool) collectDueBatches() []flushWork {
	t.queueMux.Lock()
	defer t.queueMux.Unlock()

	now := t.now()
	work := make([]flushWork, 0, len(t.queues))
	for from, q := range t.queues {
		if w, ok := t.collectQueue(from, q, now); ok {
			work = append(work, w)
		}
	}
	t.reportSize()
	return work
}

// collectQueue evaluates one EOA's queue and returns its due batch, if any. It
// applies the gating checks (idle eviction, due-time, submission spacing, a
// fresh frontier read + stale prune) and then collects at most one batch:
// the consecutive prefix takes precedence, and only when a gap at the head
// blocks it does the TTL-expiry path run. The post-gap / over-cap remainder is
// left in the queue for a later tick — never merged with this batch, since a
// head gap would fail the whole Flow transaction. Callers must hold queueMux;
// it may evict an idle empty queue as a side effect.
func (t *TxMemPool) collectQueue(
	from gethCommon.Address,
	q *eoaQueue,
	now time.Time,
) (flushWork, bool) {
	if q.isEmpty() {
		// Bound memory: drop queues with no held txs and no activity past the
		// retention period. Any in-flight submission has long since resolved
		// on-chain after this window, so discarding a lingering in-flight marker
		// here is safe — a later transaction for the EOA creates a fresh queue
		// and re-reads the index nonce.
		if now.Sub(q.lastActivity) > idleQueueRetention {
			delete(t.queues, from)
		}
		return flushWork{}, false
	}

	// Not due yet: both the sliding window and the flush deadline are still in
	// the future.
	if now.Before(q.collectionWindowEndsAt) && now.Before(q.flushDeadline) {
		return flushWork{}, false
	}

	// Safety gap since the previous submission not yet elapsed.
	if !t.spacingElapsed(q, now) {
		return flushWork{}, false
	}

	nextNonce, err := t.nonceProvider.GetNextNonce(from)
	if err != nil {
		// Exception: a local state-index nonce read should not fail under normal
		// operation. This is a background loop with no caller to reject the tx
		// to, so skip this EOA for the current tick (its batch is deferred until
		// the read succeeds) without aborting the whole flush for other EOAs.
		t.logger.Error().Err(err).Str("eoa", from.Hex()).
			Msg("unexpected failure reading nonce from local index, skipping EOA this tick")
		return flushWork{}, false
	}

	q.nonces.refreshNextNonce(nextNonce)

	// Prune transactions that can never execute: their nonce is already used
	// on-chain (e.g. filled via another gateway). They would only burn fees at
	// TTL expiry.
	t.pruneStaleTxs(q, from, nextNonce)

	if w, ok := t.collectPrefix(from, q, now, nextNonce); ok {
		return w, true
	}
	return t.collectExpired(from, q, now, nextNonce)
}

// collectPrefix detaches the longest consecutive nonce run starting at the
// expected nonce (capped at TxMaxBatchSize), marks it in flight, and re-arms the
// queue for any remainder. Returns false when a gap at the head leaves no
// eligible prefix. Callers must hold queueMux.
func (t *TxMemPool) collectPrefix(
	from gethCommon.Address,
	q *eoaQueue,
	now time.Time,
	nextNonce uint64,
) (flushWork, bool) {
	prefix := selectConsecutivePrefix(q.txs, q.nonces.expectedNonce(), t.config.TxMaxBatchSize)
	if len(prefix) == 0 {
		return flushWork{}, false
	}

	deleteByNonce(q.txs, prefix)
	// Optimistically mark the batch submitting; reconcileSubmission advances
	// submitted on success or clears it on failure.
	q.nonces.markSubmitting(prefix[len(prefix)-1].nonce)
	q.lastSubmittedAt = now
	q.lastActivity = now
	if !q.isEmpty() {
		// Re-arm for the remaining (post-gap or over-cap) txs.
		q.collectionWindowEndsAt = now.Add(t.config.TxCollectionWindow)
		q.flushDeadline = now.Add(t.config.TxSubmissionSpacing)
	}
	return flushWork{
		from:           from,
		txs:            prefix,
		needsReconcile: true,
		reason:         flushReasonPrefix,
		localNextNonce: nextNonce,
	}, true
}

// collectExpired detaches transactions held past their TTL when a head gap
// blocks the prefix path, submitting them anyway (capped at TxMaxBatchSize)
// rather than dropping them. Returns false when nothing has expired. Callers
// must hold queueMux.
//
// Rationale (no silent drops): submitting an unexecutable transaction produces a
// real, observable on-chain failure (operators can see the failed Flow
// transaction and its nonce-mismatch), whereas silently dropping it leaves no
// trace. Avoiding silent drops is the whole reason this pool exists, so an
// observable failure is strictly preferable to an invisible drop. The batch is
// deliberately NOT marked in flight: these nonces are out of order (past a gap),
// and marking them would corrupt the expected-nonce computation for future
// flushes.
//
// Tradeoff: a tx whose nonce is far ahead of the index is still submitted at TTL
// and burns fees on a guaranteed failure. How far ahead a nonce may be is
// bounded instead at Add time by TxMaxNonceGap (see classify); when that is
// unset (0), far-ahead nonces reach here.
func (t *TxMemPool) collectExpired(
	from gethCommon.Address,
	q *eoaQueue,
	now time.Time,
	nextNonce uint64,
) (flushWork, bool) {
	expired := selectExpired(q.txs, now, t.config.TxPoolTTL)
	if len(expired) > t.config.TxMaxBatchSize {
		expired = expired[:t.config.TxMaxBatchSize]
	}
	if len(expired) == 0 {
		return flushWork{}, false
	}

	deleteByNonce(q.txs, expired)
	q.lastSubmittedAt = now
	q.lastActivity = now
	batchLogFields(t.logger.Warn(), from, expired, nextNonce).
		Uint64("expected-nonce", q.nonces.expectedNonce()).
		Msg("nonce gap never filled within TTL, submitting held transactions anyway")
	return flushWork{
		from:           from,
		txs:            expired,
		reason:         flushReasonTTL,
		localNextNonce: nextNonce,
	}, true
}

// reportSize emits the pool's memory footprint: the number of per-EOA queues and
// the total number of held transactions. Callers must hold queueMux, since it
// reads t.queues (mutated concurrently, and pruned during collection).
func (t *TxMemPool) reportSize() {
	queuedTxs := 0
	for _, q := range t.queues {
		queuedTxs += q.size()
	}
	t.collector.TxPoolSize(len(t.queues), queuedTxs)
}

// pruneStaleTxs removes queued transactions whose nonce is below the current
// index nonce. They are guaranteed to fail with nonce-too-low and would only
// burn fees. Callers must hold queueMux.
func (t *TxMemPool) pruneStaleTxs(
	q *eoaQueue,
	from gethCommon.Address,
	nextNonce uint64,
) {
	var stale []heldTx
	for nonce, htx := range q.txs {
		if nonce < nextNonce {
			stale = append(stale, htx)
		}
	}
	if len(stale) > 0 {
		deleteByNonce(q.txs, stale)
		batchLogFields(t.logger.Warn(), from, stale, nextNonce).
			Msg("dropping stale transactions with nonce below the on-chain frontier")
	}
}

// submitTxBatch wraps the given (nonce-ascending) transactions in a single
// Cadence transaction and sends it to the Flow network. The run.cdc script
// uses EVM.run for a single tx and EVM.batchRun for multiple. The returned
// flow.Identifier is the wrapping Cadence tx ID: it is a deterministic hash
// computed locally over the signed tx bytes, so we can return it as soon as
// the tx is built regardless of whether the subsequent SendTransaction
// succeeded. A network failure at SendTransaction likely means the tx never
// reached the AN — the ID is still useful as a stable identifier for logs
// and any client-side retry accounting. It is zero only when the build
// itself failed (before signing).
func (t *TxMemPool) submitTxBatch(ctx context.Context, txs []heldTx) (flow.Identifier, error) {
	hexEncodedTxs := make([]cadence.Value, len(txs))
	for i, htx := range txs {
		hexEncodedTxs[i] = htx.txPayload
	}

	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return flow.Identifier{}, err
	}

	script := replaceAddresses(runTxScript, t.config.FlowNetworkID)
	// On a build/send failure the batch's EVM transactions are dropped; count
	// them here (the metric is reserved for Cadence build/submission errors) and
	// return the error. The observable WARN drop log — with eoa, nonce range and
	// flush reason — is emitted by logSubmission at the call site, which has that
	// context; this keeps submitTxBatch a pure submission primitive.
	flowTx, err := t.buildTransaction(
		ctx,
		t.getReferenceBlock(),
		script,
		cadence.NewArray(hexEncodedTxs),
		coinbaseAddress,
	)
	if err != nil {
		t.collector.TransactionsDropped(len(txs))
		return flow.Identifier{}, fmt.Errorf("building Flow transaction: %w", err)
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		t.collector.TransactionsDropped(len(txs))
		return flowTx.ID(), fmt.Errorf("sending Flow transaction: %w", err)
	}

	return flowTx.ID(), nil
}

// reconcileLoop periodically inspects each active EOA queue's most recent
// wrapping Cadence transaction and clears the in-flight nonce marker when the
// wrapper is provably not going to advance the on-chain nonce. Two cases
// trigger a reset:
//  1. The wrapper is SEALED with a non-nil Error (it reverted — e.g. the
//     intra-block reordering "nonce too high" assertion in run.cdc). This is
//     the primary DFNS-observed failure mode.
//  2. The wrapper is not sealed and more than TxReconcileStaleAfter has
//     elapsed since submission. Catches silent AN drops and any other
//     never-lands case.
//
// The "chain advanced past highestSent" case is not handled here on purpose:
// if the wrapper actually landed, it seals successfully and the next
// legitimate submission moves lastConsecutivelySubmitted forward on its own;
// there is no wedge to clear.
func (t *TxMemPool) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(t.config.TxReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce performs one pass of reconciliation across all active EOA
// queues. Callers must NOT hold queueMux — this method acquires it in short
// sections around each EOA's read/reset, and the network call (getTxResult)
// happens outside the lock so a slow AN cannot pin the pool.
func (t *TxMemPool) reconcileOnce(ctx context.Context) {
	// Snapshot the (eoa, flowTxID, lastSubmittedAt) tuples for EOAs with an
	// outstanding submission marker. Copy under the lock so we can release it
	// before doing network I/O.
	type snapshot struct {
		from            gethCommon.Address
		flowTxID        flow.Identifier
		lastSubmittedAt time.Time
	}
	var snaps []snapshot
	t.queueMux.Lock()
	for from, q := range t.queues {
		if !q.nonces.highestSent().set {
			continue
		}
		if q.lastFlowTxID == (flow.Identifier{}) {
			continue
		}
		snaps = append(snaps, snapshot{from, q.lastFlowTxID, q.lastSubmittedAt})
	}
	t.queueMux.Unlock()

	now := t.now()
	for _, s := range snaps {
		getTxResultCtx, cancel := context.WithTimeout(ctx, t.config.TxReconcileInterval)
		result, err := t.getTxResult(getTxResultCtx, s.flowTxID)
		cancel()
		// Fall through: even on error we may still want to check the staleness
		// path below. But avoid touching state on transient AN errors — only
		// reset if we have concrete evidence (SEALED-with-error) OR the
		// staleness threshold is exceeded.
		sealed := err == nil && result != nil && result.Status == flow.TransactionStatusSealed
		reverted := sealed && result.Error != nil
		stale := (!sealed || result.Error != nil) && now.Sub(s.lastSubmittedAt) > t.config.TxReconcileStaleAfter

		if !reverted && !stale {
			continue
		}

		t.queueMux.Lock()
		q, ok := t.queues[s.from]
		// If the queue was evicted or already advanced (different flow_tx_id
		// now), do nothing — a fresher submission has superseded this state.
		if !ok || q.lastFlowTxID != s.flowTxID {
			t.queueMux.Unlock()
			continue
		}
		// A newer batch entered flight between our snapshot and this reset.
		// lastFlowTxID is only advanced together with markSubmitted (in
		// reconcileSubmission), so a matching lastFlowTxID means the batch that
		// set it has already returned; any q.nonces.submitting we see now must
		// belong to a strictly newer batch. Clobbering its submitting marker
		// would let a client retry duplicate a nonce that is legitimately in
		// flight — reintroducing the very failure mode this loop exists to
		// prevent. Skip and let the next tick handle whichever wrapper needs it.
		if q.nonces.inFlight() {
			t.queueMux.Unlock()
			continue
		}

		reason := "unsealed-past-threshold"
		if reverted {
			reason = "wrapper-reverted"
		}
		elapsed := now.Sub(s.lastSubmittedAt)
		event := t.logger.Warn().
			Str("eoa", s.from.Hex()).
			Str("flow_tx_id", s.flowTxID.Hex()).
			Str("reason", reason).
			Dur("elapsed-since-submit", elapsed)

		if reverted {
			event = event.Str("wrapper-error", result.Error.Error())
		}
		event.Msg("reconciliation clearing stuck in-flight marker; subsequent Add() calls will re-classify against chain")

		q.nonces.resetSubmissionState()
		q.lastFlowTxID = flow.Identifier{}
		t.collector.TxPoolReconcileReset(reason)
		t.queueMux.Unlock()
	}
}
