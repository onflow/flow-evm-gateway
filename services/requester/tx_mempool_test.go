package requester

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"math"
	"math/big"
	"sync"
	"testing"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

func makeHeldTx(nonce uint64, enqueuedAt time.Time) heldTx {
	return heldTx{
		txHash:     gethCommon.BytesToHash([]byte{byte(nonce)}),
		nonce:      nonce,
		enqueuedAt: enqueuedAt,
	}
}

func Test_SelectConsecutivePrefix(t *testing.T) {
	now := time.Now()

	t.Run("empty queue returns empty prefix", func(t *testing.T) {
		prefix := selectConsecutivePrefix(map[uint64]heldTx{}, 0, 5)
		assert.Empty(t, prefix)
	})

	t.Run("full consecutive run from expected nonce", func(t *testing.T) {
		txs := map[uint64]heldTx{
			0: makeHeldTx(0, now), 1: makeHeldTx(1, now), 2: makeHeldTx(2, now),
		}
		prefix := selectConsecutivePrefix(txs, 0, 5)
		assert.Len(t, prefix, 3)
		assert.Equal(t, uint64(0), prefix[0].nonce)
		assert.Equal(t, uint64(1), prefix[1].nonce)
		assert.Equal(t, uint64(2), prefix[2].nonce)
	})

	t.Run("stops at first gap", func(t *testing.T) {
		txs := map[uint64]heldTx{
			1: makeHeldTx(1, now), 2: makeHeldTx(2, now),
			4: makeHeldTx(4, now), 5: makeHeldTx(5, now),
		}
		prefix := selectConsecutivePrefix(txs, 1, 5)
		assert.Len(t, prefix, 2)
		assert.Equal(t, uint64(1), prefix[0].nonce)
		assert.Equal(t, uint64(2), prefix[1].nonce)
	})

	t.Run("gap at the head returns empty prefix", func(t *testing.T) {
		txs := map[uint64]heldTx{
			5: makeHeldTx(5, now), 6: makeHeldTx(6, now),
		}
		prefix := selectConsecutivePrefix(txs, 3, 5)
		assert.Empty(t, prefix)
	})

	t.Run("caps at maxBatch", func(t *testing.T) {
		txs := map[uint64]heldTx{}
		for n := uint64(0); n < 10; n++ {
			txs[n] = makeHeldTx(n, now)
		}
		prefix := selectConsecutivePrefix(txs, 0, 5)
		assert.Len(t, prefix, 5)
		assert.Equal(t, uint64(4), prefix[4].nonce)
	})
}

func Test_SelectExpired(t *testing.T) {
	now := time.Now()
	ttl := 30 * time.Second

	t.Run("nothing expired", func(t *testing.T) {
		txs := map[uint64]heldTx{
			3: makeHeldTx(3, now.Add(-time.Second)),
		}
		assert.Empty(t, selectExpired(txs, now, ttl))
	})

	t.Run("expired txs returned sorted by nonce", func(t *testing.T) {
		txs := map[uint64]heldTx{
			7: makeHeldTx(7, now.Add(-time.Minute)),
			3: makeHeldTx(3, now.Add(-time.Minute)),
			5: makeHeldTx(5, now.Add(-time.Second)), // not expired
		}
		expired := selectExpired(txs, now, ttl)
		assert.Len(t, expired, 2)
		assert.Equal(t, uint64(3), expired[0].nonce)
		assert.Equal(t, uint64(7), expired[1].nonce)
	})
}

type fakeNonceProvider struct {
	nonce uint64
	err   error
}

// GetNextNonce satisfies NonceProvider; GetNonce satisfies the NonceView that
// GetBlockView returns. Both read every EOA's nonce as f.nonce/f.err.
func (f *fakeNonceProvider) GetNextNonce(_ gethCommon.Address) (uint64, error) {
	return f.nonce, f.err
}

func (f *fakeNonceProvider) GetNonce(_ gethCommon.Address) (uint64, error) {
	return f.nonce, f.err
}

// GetBlockView returns the fake itself as the NonceView: a single fake reads
// every EOA's nonce as f.nonce/f.err, mirroring the production single-view
// read path.
func (f *fakeNonceProvider) GetBlockView() (NonceView, error) {
	return f, nil
}

func newTestPool(
	np NonceProvider,
	submit func(context.Context, []heldTx) error,
	cfg config.Config,
) *TxMemPool {
	pool := &TxMemPool{
		SingleTxPool: &SingleTxPool{
			logger:      zerolog.Nop(),
			txPublisher: models.NewPublisher[*gethTypes.Transaction](),
			config:      cfg,
			collector:   metrics.NopCollector,
		},
		nonceProvider: np,
		queues:        make(map[gethCommon.Address]*eoaQueue),
		now:           time.Now,
	}
	pool.submitBatch = submit
	return pool
}

// fakeClock is a controllable clock for driving the pool's time-based behavior
// in tests without wall-clock sleeps. It is safe for the single-goroutine unit
// tests here (Add and collectDueBatches are called synchronously).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testPoolConfig() config.Config {
	return config.Config{
		TxCollectionWindow:  100 * time.Millisecond,
		TxSubmissionSpacing: time.Second,
		TxPoolTTL:           time.Minute,
		TxMaxBatchSize:      10,
	}
}

// signedTestTx returns a signed legacy transaction with the given nonce and
// value (the value only matters when two distinct txs with the same nonce
// are needed).
func signedTestTx(
	t *testing.T,
	key *ecdsa.PrivateKey,
	nonce uint64,
	value int64,
) *gethTypes.Transaction {
	t.Helper()
	chainID := big.NewInt(747)
	tx, err := gethTypes.SignTx(
		gethTypes.NewTransaction(
			nonce,
			gethCommon.HexToAddress("0x0000000000000000000000000000000000000001"),
			big.NewInt(value),
			21_000,
			big.NewInt(1),
			nil,
		),
		gethTypes.LatestSignerForChainID(chainID),
		key,
	)
	require.NoError(t, err)
	return tx
}

func Test_TxMemPool_FastPathSubmitsImmediately(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	var submitted [][]heldTx
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, txs []heldTx) error {
			submitted = append(submitted, txs)
			return nil
		},
		testPoolConfig(),
	)

	tx := signedTestTx(t, key, 0, 1)
	require.NoError(t, pool.Add(context.Background(), tx))

	require.Len(t, submitted, 1)
	require.Len(t, submitted[0], 1)
	assert.Equal(t, tx.Hash(), submitted[0][0].txHash)

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.Empty(t, q.txs)
	// Explicit success update: submitting cleared, submitted advanced to 0.
	assert.False(t, q.nonces.inFlight())
	assert.Equal(t, toNonceWrapper(0), q.nonces.lastConsecutivelySubmitted)
}

// The fast-path submit must run under a bounded-deadline context so a hung
// Flow call cannot pin the pool-wide queueMux indefinitely (audit liveness
// finding). Before the bound, Add forwarded its raw context (here Background,
// with no deadline) straight into the submit.
func Test_TxMemPool_FastPathSubmitHasBoundedTimeout(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	var deadline time.Time
	var hasDeadline bool
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(ctx context.Context, _ []heldTx) error {
			deadline, hasDeadline = ctx.Deadline()
			return nil
		},
		testPoolConfig(),
	)

	before := time.Now()
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 0, 1)))
	after := time.Now()

	require.True(t, hasDeadline, "fast-path submit context must carry a deadline")
	assert.Greater(t, deadline, before)
	assert.LessOrEqual(t, deadline, after.Add(fastPathSubmitTimeout),
		"deadline must be bounded by fastPathSubmitTimeout")
}

func Test_TxMemPool_UnexpectedNonceEnqueues(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	submitCalls := 0
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error {
			submitCalls++
			return nil
		},
		testPoolConfig(),
	)

	tx := signedTestTx(t, key, 5, 1)
	require.NoError(t, pool.Add(context.Background(), tx))

	assert.Zero(t, submitCalls)
	q := pool.queues[from]
	require.NotNil(t, q)
	held, ok := q.txs[5]
	require.True(t, ok)
	assert.Equal(t, tx.Hash(), held.txHash)
	assert.False(t, q.nonces.inFlight())
}

func Test_TxMemPool_NonceReadErrorRejectsTx(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	// A local state-index nonce read should never fail; when it does it is
	// an exception, and Add must reject the tx rather than enqueue it.
	nonceErr := errors.New("index read failed")
	submitCalls := 0
	pool := newTestPool(
		&fakeNonceProvider{err: nonceErr},
		func(_ context.Context, _ []heldTx) error {
			submitCalls++
			return nil
		},
		testPoolConfig(),
	)

	// A fresh EOA Add (empty queue, spacing elapsed) triggers the nonce read.
	err = pool.Add(context.Background(), signedTestTx(t, key, 0, 1))
	require.ErrorIs(t, err, nonceErr)

	assert.Zero(t, submitCalls)
	q := pool.queues[from]
	require.NotNil(t, q)
	assert.Empty(t, q.txs)
	assert.False(t, q.nonces.inFlight())
}

func Test_TxMemPool_InFlightDuplicateRejected(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)

	// Fast-path submit of nonce 0; the index still reports 0.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 0, 1)))

	// A different transaction with the same nonce must be rejected while the
	// first one is in flight.
	err = pool.Add(context.Background(), signedTestTx(t, key, 0, 2))
	assert.ErrorIs(t, err, errs.ErrInFlightNonce)
}

// Case 9 (duplicate): re-adding the IDENTICAL transaction (same nonce AND same
// hash) of one already HELD in the queue is rejected with
// ErrDuplicateTransaction, while a same-nonce tx with a DIFFERENT hash replaces
// the queued one (last write wins). This exercises the duplicate check itself,
// distinct from the in-flight rejection above (which removes the tx from the
// queue before the second Add).
func Test_TxMemPool_DuplicateQueuedTxRejected(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	pool := newTestPool(
		// Frontier 0, so a nonce-5 tx is out of order and is HELD (not submitted).
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)

	tx := signedTestTx(t, key, 5, 1)
	require.NoError(t, pool.Add(context.Background(), tx))

	// Re-adding the identical tx (same hash) is a duplicate.
	err = pool.Add(context.Background(), tx)
	assert.ErrorIs(t, err, errs.ErrDuplicateTransaction)

	// A same-nonce, different-payload (different hash) tx is NOT a duplicate; it
	// replaces the held one.
	replacement := signedTestTx(t, key, 5, 2)
	require.NotEqual(t, tx.Hash(), replacement.Hash())
	require.NoError(t, pool.Add(context.Background(), replacement))
	assert.Equal(t, replacement.Hash(), pool.queues[from].txs[5].txHash)
}

func Test_TxMemPool_FailedFlushDoesNotWedgeEOA(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	submitErr := errors.New("network down")
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return submitErr },
		testPoolConfig(),
	)

	// Queue txs with nonces 0 and 1, deadlines already in the past so the
	// batch is due.
	past := time.Now().Add(-time.Second)
	pool.queues[from] = &eoaQueue{
		txs: map[uint64]heldTx{
			0: {txHash: signedTestTx(t, key, 0, 1).Hash(), nonce: 0, enqueuedAt: past},
			1: {txHash: signedTestTx(t, key, 1, 1).Hash(), nonce: 1, enqueuedAt: past},
		},
		collectionWindowEndsAt: past,
		flushDeadline:          past,
	}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	assert.True(t, work[0].needsReconcile)
	require.Len(t, work[0].txs, 2)

	// State was committed optimistically under the lock.
	q := pool.queues[from]
	require.True(t, q.nonces.inFlight())
	assert.Equal(t, toNonceWrapper(1), q.nonces.submitting)

	// The submission fails; submitWork must clear the in-flight marker.
	err = pool.submitWork(context.Background(), work[0])
	require.ErrorIs(t, err, submitErr)
	assert.False(t, q.nonces.inFlight())

	// A resubmission of the failed nonce must NOT be rejected as in flight.
	err = pool.Add(context.Background(), signedTestTx(t, key, 0, 2))
	assert.NotErrorIs(t, err, errs.ErrInFlightNonce)
}

func Test_ReconcileSubmission_OnlyReconcilesMatchingInFlightBatch(t *testing.T) {
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	from := gethCommon.HexToAddress("0xabc")
	pool.queues[from] = &eoaQueue{
		txs:    map[uint64]heldTx{},
		nonces: nonceTracker{submitting: toNonceWrapper(7)},
	}
	submitErr := errors.New("network down")

	// A different (newer) in-flight nonce owns the marker: not cleared.
	pool.reconcileSubmission(
		flushWork{from: from, txs: []heldTx{makeHeldTx(5, time.Time{})}, needsReconcile: true},
		submitErr,
	)
	assert.True(t, pool.queues[from].nonces.inFlight())

	// A TTL-expiry batch (needsReconcile false) never touches the tracker.
	pool.reconcileSubmission(
		flushWork{from: from, txs: []heldTx{makeHeldTx(7, time.Time{})}, needsReconcile: false},
		submitErr,
	)
	assert.True(t, pool.queues[from].nonces.inFlight())

	// The failed in-flight batch still owns the marker: cleared.
	pool.reconcileSubmission(
		flushWork{from: from, txs: []heldTx{makeHeldTx(7, time.Time{})}, needsReconcile: true},
		submitErr,
	)
	assert.False(t, pool.queues[from].nonces.inFlight())

	// Unknown EOA: no panic.
	pool.reconcileSubmission(
		flushWork{from: gethCommon.HexToAddress("0xdef"), txs: []heldTx{makeHeldTx(7, time.Time{})}, needsReconcile: true},
		submitErr,
	)
}

// A successful submission advances the consecutively-submitted nonce and clears
// the in-flight marker (the explicit success update).
func Test_TxMemPool_SuccessfulFlushMarksSubmitted(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)

	past := time.Now().Add(-time.Second)
	pool.queues[from] = &eoaQueue{
		txs: map[uint64]heldTx{
			0: {txHash: signedTestTx(t, key, 0, 1).Hash(), nonce: 0, enqueuedAt: past},
			1: {txHash: signedTestTx(t, key, 1, 1).Hash(), nonce: 1, enqueuedAt: past},
		},
		collectionWindowEndsAt: past,
		flushDeadline:          past,
	}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)

	require.NoError(t, pool.submitWork(context.Background(), work[0]))

	q := pool.queues[from]
	assert.False(t, q.nonces.inFlight())
	assert.Equal(t, toNonceWrapper(1), q.nonces.lastConsecutivelySubmitted)
}

// No silent drops: a failed flush submission must produce an observable WARN log
// carrying the dropped tx hashes and enough context to debug a "lost
// transaction" report (eoa, nonce range, next nonce, batch size, reason,
// error).
func Test_TxMemPool_FailedSubmitLogsDropWarning(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	var logBuf bytes.Buffer
	submitErr := errors.New("network down")
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return submitErr },
		testPoolConfig(),
	)
	pool.logger = zerolog.New(&logBuf)

	tx0 := signedTestTx(t, key, 0, 1)
	tx1 := signedTestTx(t, key, 1, 1)
	past := time.Now().Add(-time.Second)
	pool.queues[from] = &eoaQueue{
		txs: map[uint64]heldTx{
			0: {txHash: tx0.Hash(), nonce: 0, enqueuedAt: past},
			1: {txHash: tx1.Hash(), nonce: 1, enqueuedAt: past},
		},
		collectionWindowEndsAt: past,
		flushDeadline:          past,
	}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	require.Error(t, pool.submitWork(context.Background(), work[0]))

	out := logBuf.String()
	assert.Contains(t, out, `"level":"warn"`, "drop must be observable at WARN level")
	assert.Contains(t, out, tx0.Hash().Hex(), "dropped tx hash must be logged")
	assert.Contains(t, out, tx1.Hash().Hex(), "dropped tx hash must be logged")
	assert.Contains(t, out, from.Hex(), "eoa must be logged")
	assert.Contains(t, out, `"local-next-nonce":0`, "next nonce must be logged")
	assert.Contains(t, out, `"batch-size":2`, "batch size must be logged")
	assert.Contains(t, out, "network down", "submit error must be logged")
}

// A successful submission is traceable via a DEBUG log carrying the eoa and
// nonce range, so a sent batch can be found in logs without a warning.
func Test_TxMemPool_SuccessfulSubmitLogsDebug(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	var logBuf bytes.Buffer
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.logger = zerolog.New(&logBuf)

	tx0 := signedTestTx(t, key, 0, 1)
	tx1 := signedTestTx(t, key, 1, 1)
	past := time.Now().Add(-time.Second)
	pool.queues[from] = &eoaQueue{
		txs: map[uint64]heldTx{
			0: {txHash: tx0.Hash(), nonce: 0, enqueuedAt: past},
			1: {txHash: tx1.Hash(), nonce: 1, enqueuedAt: past},
		},
		collectionWindowEndsAt: past,
		flushDeadline:          past,
	}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	require.NoError(t, pool.submitWork(context.Background(), work[0]))

	out := logBuf.String()
	assert.Contains(t, out, `"level":"debug"`, "successful send must be traceable at DEBUG level")
	assert.Contains(t, out, from.Hex(), "eoa must be logged")
	assert.Contains(t, out, `"low-nonce":0`)
	assert.Contains(t, out, `"high-nonce":1`)
}

// A failed fast-path submission must not rate-limit the EOA via lastSubmittedAt,
// and must leave nothing in flight.
func Test_TxMemPool_FailedFastPathLeavesNoState(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	submitErr := errors.New("network down")
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return submitErr },
		testPoolConfig(),
	)

	err = pool.Add(context.Background(), signedTestTx(t, key, 0, 1))
	require.ErrorIs(t, err, submitErr)

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.False(t, q.nonces.inFlight())
	assert.False(t, q.nonces.lastConsecutivelySubmitted.set, "failed submission must not advance submitted")
	assert.True(t, q.lastSubmittedAt.IsZero(), "failed submission must not stamp lastSubmittedAt")
}

// Resubmitting a single held tx with the same nonce must not re-arm the flush
// deadline anchored at first enqueue.
func Test_TxMemPool_SameNonceReplacementKeepsFlushDeadline(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	pool := newTestPool(
		// Index reports 0 so a nonce-5 tx is out of order and gets queued.
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)

	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 5, 1)))
	firstDeadline := pool.queues[from].flushDeadline

	// Replace the same nonce with a different payload; the deadline must hold.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 5, 2)))
	assert.Equal(t, firstDeadline, pool.queues[from].flushDeadline)
}

// TTL-expiry flushes are capped at TxMaxBatchSize.
func Test_TxMemPool_TTLFlushCappedAtMaxBatch(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	cfg := testPoolConfig()
	cfg.TxMaxBatchSize = 3
	cfg.TxPoolTTL = time.Millisecond

	pool := newTestPool(
		// Index nonce 0, but the held txs start at nonce 10 — a permanent gap
		// at the head, so the only flush path is TTL expiry.
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		cfg,
	)

	past := time.Now().Add(-time.Second)
	txs := map[uint64]heldTx{}
	for n := uint64(10); n < 17; n++ { // 7 expired txs
		txs[n] = makeHeldTx(n, past)
	}
	pool.queues[from] = &eoaQueue{txs: txs, collectionWindowEndsAt: past, flushDeadline: past}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	assert.Len(t, work[0].txs, 3, "TTL flush must be capped at TxMaxBatchSize")
	assert.False(t, work[0].needsReconcile)
	// Lowest nonces drained first; remainder stays queued.
	assert.Equal(t, uint64(10), work[0].txs[0].nonce)
	assert.Len(t, pool.queues[from].txs, 4)
}

// A queue emptied without ever submitting (e.g. all txs pruned) must still age
// out via lastActivity rather than leaking forever.
func Test_TxMemPool_EmptyQueueAgesOut(t *testing.T) {
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	from := gethCommon.HexToAddress("0xabc")

	// Empty queue, never submitted, but with a lingering in-flight marker and
	// last active beyond the retention window: must be removed.
	pool.queues[from] = &eoaQueue{
		txs:          map[uint64]heldTx{},
		nonces:       nonceTracker{submitting: toNonceWrapper(5)},
		lastActivity: time.Now().Add(-2 * idleQueueRetention),
	}
	pool.collectDueBatches()
	_, ok := pool.queues[from]
	assert.False(t, ok, "idle empty queue must be removed regardless of in-flight/never-submitted state")

	// A recently-active empty queue is retained.
	pool.queues[from] = &eoaQueue{txs: map[uint64]heldTx{}, lastActivity: time.Now()}
	pool.collectDueBatches()
	_, ok = pool.queues[from]
	assert.True(t, ok, "recently active queue must be retained")
}

// countingCollector embeds NopCollector and counts the metric calls relevant
// to the pruning behavior under test.
type countingCollector struct {
	metrics.Collector
	droppedCount  int
	txPoolQueues  int
	txPoolQueued  int
	txPoolSizeSet bool
}

func (c *countingCollector) TransactionsDropped(count int) {
	c.droppedCount += count
}

func (c *countingCollector) TxPoolSize(queues int, queued int) {
	c.txPoolQueues = queues
	c.txPoolQueued = queued
	c.txPoolSizeSet = true
}

// Stale txs (nonce below the next nonce) must be pruned from the queue, but
// pruning must NOT increment the TransactionsDropped metric — that metric is
// reserved for build/submission errors of the Cadence transaction to Flow.
func Test_TxMemPool_PruneStaleDoesNotIncrementDropped(t *testing.T) {
	collector := &countingCollector{Collector: metrics.NopCollector}
	pool := newTestPool(
		// Index reports nonce 5; held txs below 5 are stale.
		&fakeNonceProvider{nonce: 5},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.collector = collector

	from := gethCommon.HexToAddress("0xabc")
	q := &eoaQueue{txs: map[uint64]heldTx{
		2: makeHeldTx(2, time.Now()),
		3: makeHeldTx(3, time.Now()),
		5: makeHeldTx(5, time.Now()),
	}}

	pool.queueMux.Lock()
	pool.pruneStaleTxs(q, from, 5)
	pool.queueMux.Unlock()

	// Stale nonces 2 and 3 pruned; nonce 5 retained.
	assert.Len(t, q.txs, 1)
	_, ok := q.txs[5]
	assert.True(t, ok)
	assert.Zero(t, collector.droppedCount, "pruning stale txs must not increment TransactionsDropped")
}

// collectDueBatches must report the pool's memory footprint via TxPoolSize each
// time it runs.
func Test_TxMemPool_CollectDueBatchesReportsSize(t *testing.T) {
	collector := &countingCollector{Collector: metrics.NopCollector}
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.collector = collector

	// Two EOAs, neither due (deadlines in the future), holding 3 txs total.
	future := time.Now().Add(time.Hour)
	pool.queues[gethCommon.HexToAddress("0xaaa")] = &eoaQueue{
		txs:                    map[uint64]heldTx{1: makeHeldTx(1, time.Now())},
		collectionWindowEndsAt: future,
		flushDeadline:          future,
	}
	pool.queues[gethCommon.HexToAddress("0xbbb")] = &eoaQueue{
		txs:                    map[uint64]heldTx{2: makeHeldTx(2, time.Now()), 3: makeHeldTx(3, time.Now())},
		collectionWindowEndsAt: future,
		flushDeadline:          future,
	}

	pool.collectDueBatches()

	assert.True(t, collector.txPoolSizeSet)
	assert.Equal(t, 2, collector.txPoolQueues)
	assert.Equal(t, 3, collector.txPoolQueued)
}

func Test_NonceTracker_Classify(t *testing.T) {
	// classify reads the on-chain frontier from the provider, so the frontier
	// is supplied via `frontier` (not set directly on the tracker). In-flight
	// cases return before the read, so their frontier value is irrelevant.
	tests := []struct {
		name     string
		tracker  nonceTracker
		frontier uint64
		nonce    uint64
		want     nonceVerdict
	}{
		{"nonce at the frontier is next-expected", nonceTracker{}, 5, 5, nonceNextExpected},
		{"gap ahead queues", nonceTracker{}, 5, 7, nonceQueue},
		{"below index is too low (no gap configured)", nonceTracker{}, 5, 3, nonceTooLow},
		{"nonce 0 is next-expected on a zero tracker", nonceTracker{}, 0, 0, nonceNextExpected},
		{"at submitted is in-flight", nonceTracker{lastConsecutivelySubmitted: toNonceWrapper(6)}, 5, 6, nonceInFlight},
		{"below submitted is in-flight", nonceTracker{lastConsecutivelySubmitted: toNonceWrapper(6)}, 5, 4, nonceInFlight},
		{"next after submitted is expected", nonceTracker{lastConsecutivelySubmitted: toNonceWrapper(6)}, 5, 7, nonceNextExpected},
		{"at submitting is in-flight", nonceTracker{submitting: toNonceWrapper(8)}, 5, 8, nonceInFlight},
		{"next after submitting is expected", nonceTracker{submitting: toNonceWrapper(8)}, 5, 9, nonceNextExpected},
		{"index ahead of submitted: expected follows index", nonceTracker{lastConsecutivelySubmitted: toNonceWrapper(6)}, 10, 10, nonceNextExpected},
		{"index ahead of submitted: below index is too low", nonceTracker{lastConsecutivelySubmitted: toNonceWrapper(6)}, 10, 8, nonceTooLow},
		// Range checks (maxNonceGap > 0).
		{"gap: index nonce is next-expected", nonceTracker{maxNonceGap: 50}, 5, 5, nonceNextExpected},
		{"gap: below index is too low", nonceTracker{maxNonceGap: 50}, 5, 4, nonceTooLow},
		{"gap: at the upper bound is accepted (queued)", nonceTracker{maxNonceGap: 50}, 5, 55, nonceQueue},
		{"gap: beyond the upper bound is too high", nonceTracker{maxNonceGap: 50}, 5, 56, nonceTooHigh},
		{"gap: in-flight takes precedence over too-low", nonceTracker{maxNonceGap: 50, lastConsecutivelySubmitted: toNonceWrapper(6)}, 5, 3, nonceInFlight},
		{"no gap: far-ahead nonce queues (no upper bound)", nonceTracker{}, 5, 100_000, nonceQueue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracker := tc.tracker
			// classify expects a normalized gap (0 in the table means "unbounded",
			// stored as math.MaxUint64), matching how queues are constructed.
			tracker.maxNonceGap = normalizeNonceGap(tracker.maxNonceGap)
			got, err := tracker.classify(
				tc.nonce,
				&fakeNonceProvider{nonce: tc.frontier},
				gethCommon.HexToAddress("0xabc"),
			)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// normalizeNonceGap maps the config "0 = no upper bound" to MaxUint64 (so
// classify needs no per-call gap>0 guard) and passes other values through.
func Test_NormalizeNonceGap(t *testing.T) {
	assert.Equal(t, uint64(math.MaxUint64), normalizeNonceGap(0), "0 = unbounded maps to MaxUint64")
	assert.Equal(t, uint64(500), normalizeNonceGap(500))
}

// With a configured max gap, Add rejects out-of-range nonces up front.
func Test_TxMemPool_RejectsNonceOutOfRange(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	cfg := testPoolConfig()
	cfg.TxMaxNonceGap = 50

	submitCalls := 0
	pool := newTestPool(
		&fakeNonceProvider{nonce: 100}, // on-chain frontier at nonce 100
		func(_ context.Context, _ []heldTx) error { submitCalls++; return nil },
		cfg,
	)
	ctx := context.Background()

	// Below the frontier: already used.
	require.ErrorIs(t, pool.Add(ctx, signedTestTx(t, key, 99, 1)), errs.ErrNonceTooLow)

	// More than maxNonceGap (50) ahead of the frontier.
	require.ErrorIs(t, pool.Add(ctx, signedTestTx(t, key, 200, 1)), errs.ErrNonceTooHigh)

	// Within the accepted window (ahead of expected, but <= frontier+gap): held.
	require.NoError(t, pool.Add(ctx, signedTestTx(t, key, 120, 1)))

	assert.Zero(t, submitCalls, "out-of-order/rejected txs are never fast-path submitted")
}

func Test_NonceTracker_ExpectedNonce(t *testing.T) {
	assert.Equal(t, uint64(5), (&nonceTracker{localNextNonce: 5}).expectedNonce())
	assert.Equal(t, uint64(7),
		(&nonceTracker{localNextNonce: 5, lastConsecutivelySubmitted: toNonceWrapper(6)}).expectedNonce())
	assert.Equal(t, uint64(9),
		(&nonceTracker{localNextNonce: 5, submitting: toNonceWrapper(8)}).expectedNonce())
	// The on-chain frontier wins when it is ahead of our own sends.
	assert.Equal(t, uint64(10),
		(&nonceTracker{localNextNonce: 10, lastConsecutivelySubmitted: toNonceWrapper(6)}).expectedNonce())
}

func Test_NonceTracker_Transitions(t *testing.T) {
	n := &nonceTracker{localNextNonce: 5}

	// markSubmitting sets the in-flight marker.
	n.markSubmitting(7)
	assert.True(t, n.inFlight())
	assert.Equal(t, toNonceWrapper(7), n.submitting)

	// markSubmitted advances submitted and clears submitting.
	n.markSubmitted(7)
	assert.False(t, n.inFlight())
	assert.Equal(t, toNonceWrapper(7), n.lastConsecutivelySubmitted)

	// rollbackSubmitting only clears a matching in-flight nonce.
	n.markSubmitting(9)
	n.rollbackSubmitting(8) // non-matching: no-op
	assert.True(t, n.inFlight())
	n.rollbackSubmitting(9) // matching: cleared
	assert.False(t, n.inFlight())
	// A rollback never disturbs the consecutively-submitted nonce.
	assert.Equal(t, toNonceWrapper(7), n.lastConsecutivelySubmitted)

	// refreshNextNonce updates the cached frontier.
	n.refreshNextNonce(12)
	assert.Equal(t, uint64(12), n.localNextNonce)
}

// A stale success ack (e.g. once submissions run concurrently and a newer batch
// has already been marked in flight) must not clear the newer in-flight marker
// or regress the consecutively-submitted nonce.
func Test_NonceTracker_StaleMarkSubmittedDoesNotClobber(t *testing.T) {
	n := &nonceTracker{}

	n.markSubmitting(6) // batch A {.,6} in flight
	n.markSubmitting(8) // batch B {7,8} replaces it as the newer in-flight batch

	// Batch A's (stale) success ack arrives: it must not clear B's marker.
	n.markSubmitted(6)
	assert.True(t, n.inFlight(), "newer in-flight marker must survive a stale ack")
	assert.Equal(t, toNonceWrapper(8), n.submitting)
	assert.Equal(t, toNonceWrapper(6), n.lastConsecutivelySubmitted)

	// Batch B's ack then clears the marker and advances submitted to 8.
	n.markSubmitted(8)
	assert.False(t, n.inFlight())
	assert.Equal(t, toNonceWrapper(8), n.lastConsecutivelySubmitted)

	// An out-of-order ack for an older nonce must not regress submitted.
	n.markSubmitted(7)
	assert.Equal(t, toNonceWrapper(8), n.lastConsecutivelySubmitted, "submitted must never regress")
}

// --- Clock-driven timing tests -------------------------------------------
// These drive the collection window, flush deadline, submission spacing, TTL
// expiry and idle-queue retention through the real Add -> collectDueBatches
// path using an injected clock, rather than hand-building deadline timestamps.

var timingClockBase = time.Unix(1_700_000_000, 0)

// The sliding collection window is re-armed on every arrival.
func Test_TxMemPool_CollectionWindowResetsOnEachArrival(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	// A gapped nonce (frontier 0, nonce 5) queues rather than fast-pathing.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 5, 1)))
	q := pool.queues[from]
	require.NotNil(t, q)
	assert.Equal(t, timingClockBase.Add(testPoolConfig().TxCollectionWindow), q.collectionWindowEndsAt)
	firstDeadline := q.flushDeadline

	// A later arrival re-arms the window relative to its own arrival time, but
	// must NOT re-arm the first-enqueue flush deadline.
	clk.advance(40 * time.Millisecond)
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 6, 1)))
	assert.Equal(t, clk.now().Add(testPoolConfig().TxCollectionWindow), q.collectionWindowEndsAt)
	assert.Equal(t, firstDeadline, q.flushDeadline, "flush deadline stays anchored at first enqueue")
}

// Submission spacing gates the background flush: a due batch is held until
// TxSubmissionSpacing has elapsed since the previous submission, then flushed.
func Test_TxMemPool_SpacingGateDefersThenFlushes(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	clk := newFakeClock(timingClockBase)
	var submitted [][]heldTx
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, txs []heldTx) error {
			submitted = append(submitted, txs)
			return nil
		},
		testPoolConfig(),
	)
	pool.now = clk.now
	cfg := testPoolConfig()

	// nonce 0 fast-paths immediately and stamps lastSubmittedAt = base.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 0, 1)))
	require.Len(t, submitted, 1)
	// nonces 1,2 arrive while spacing has not elapsed, so they queue.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 1, 1)))
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 2, 1)))

	// Past the collection window but within the spacing gap: NOT flushed yet.
	clk.advance(cfg.TxCollectionWindow + 50*time.Millisecond)
	pool.collectDueBatches()
	assert.Len(t, submitted, 1, "spacing gate must defer the flush")
	assert.Len(t, pool.queues[from].txs, 2)

	// Once spacing has elapsed, the consecutive prefix {1,2} flushes.
	clk.advance(cfg.TxSubmissionSpacing)
	for _, w := range pool.collectDueBatches() {
		require.NoError(t, pool.submitWork(context.Background(), w))
	}
	require.Len(t, submitted, 2)
	assert.Equal(t, []uint64{1, 2}, noncesOf(submitted[1]))
	assert.Empty(t, pool.queues[from].txs)
}

// The first-enqueue flush deadline forces a flush even while the (continuously
// re-armed) collection window is still in the future.
func Test_TxMemPool_FlushDeadlineForcesFlushWhileWindowOpen(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	clk := newFakeClock(timingClockBase)
	var submitted [][]heldTx
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, txs []heldTx) error {
			submitted = append(submitted, txs)
			return nil
		},
		testPoolConfig(),
	)
	pool.now = clk.now
	cfg := testPoolConfig()

	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 0, 1))) // fast-path
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 1, 1))) // queues; deadline = base + spacing

	// Just before the deadline, re-arm the window with a late arrival so the
	// window remains in the future at flush time.
	clk.advance(cfg.TxSubmissionSpacing - 50*time.Millisecond)
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 2, 1)))
	q := pool.queues[from]
	require.True(t, clk.now().Before(q.collectionWindowEndsAt), "window must still be open")

	// Cross the deadline (still before the window end): the deadline forces it.
	clk.advance(60 * time.Millisecond)
	require.True(t, clk.now().Before(q.collectionWindowEndsAt), "window still open at flush time")
	require.False(t, clk.now().Before(q.flushDeadline), "deadline has passed")
	for _, w := range pool.collectDueBatches() {
		require.NoError(t, pool.submitWork(context.Background(), w))
	}
	require.Len(t, submitted, 2)
	assert.Equal(t, []uint64{1, 2}, noncesOf(submitted[1]))
}

// A nonce stuck behind a permanent head gap is submitted anyway once TxPoolTTL
// elapses (driven by the clock via enqueuedAt), not silently dropped.
func Test_TxMemPool_TTLExpiryViaClock(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	clk := newFakeClock(timingClockBase)
	var submitted []flushWork
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now
	cfg := testPoolConfig()

	// Frontier 0, nonce 10 — a permanent head gap, so the only exit is TTL.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 10, 1)))

	// Before TTL: held, not submitted.
	clk.advance(cfg.TxCollectionWindow + 50*time.Millisecond)
	assert.Empty(t, pool.collectDueBatches())
	assert.Len(t, pool.queues[from].txs, 1)

	// Past TTL: submitted anyway, as a non-in-flight TTL batch.
	clk.advance(cfg.TxPoolTTL)
	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	submitted = work
	assert.False(t, submitted[0].needsReconcile)
	assert.Equal(t, flushReasonTTL, submitted[0].reason)
	assert.Equal(t, []uint64{10}, noncesOf(submitted[0].txs))
	assert.Empty(t, pool.queues[from].txs)
}

// An empty queue with no activity past idleQueueRetention is evicted.
func Test_TxMemPool_IdleQueueEvictedViaClock(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	// nonce 0 fast-paths and leaves an empty queue with lastActivity = base.
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 0, 1)))
	require.NotNil(t, pool.queues[from])

	// Within retention: kept.
	clk.advance(idleQueueRetention / 2)
	pool.collectDueBatches()
	assert.NotNil(t, pool.queues[from])

	// Past retention: evicted.
	clk.advance(idleQueueRetention)
	pool.collectDueBatches()
	_, ok := pool.queues[from]
	assert.False(t, ok, "idle empty queue must be evicted past retention")
}

// noncesOf extracts the nonces of a batch in order, for concise assertions.
func noncesOf(txs []heldTx) []uint64 {
	ns := make([]uint64, len(txs))
	for i, htx := range txs {
		ns[i] = htx.nonce
	}
	return ns
}
