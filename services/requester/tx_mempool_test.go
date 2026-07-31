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
	"github.com/onflow/flow-go-sdk"
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
	// Adapt the old-style submit closure to the new signature that carries the
	// wrapping Cadence tx ID. Tests that need to assert on the flowTxID field
	// override pool.submitBatch directly after construction.
	pool.submitBatch = func(ctx context.Context, txs []heldTx) (flow.Identifier, error) {
		return flow.Identifier{}, submit(ctx, txs)
	}
	// The reconciliation loop is not started by newTestPool, but the field is
	// defaulted to a no-op fake so any direct call to reconcileOnce from a test
	// works without a live Access Node.
	pool.getTxResult = func(context.Context, flow.Identifier) (*flow.TransactionResult, error) {
		return nil, nil
	}
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
		TxCollectionWindow:    100 * time.Millisecond,
		TxSubmissionSpacing:   time.Second,
		TxPoolTTL:             time.Minute,
		TxMaxBatchSize:        10,
		TxReconcileInterval:   time.Second,
		TxReconcileStaleAfter: 30 * time.Second,
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
		flow.Identifier{},
		submitErr,
	)
	assert.True(t, pool.queues[from].nonces.inFlight())

	// A TTL-expiry batch (needsReconcile false) never touches the tracker.
	pool.reconcileSubmission(
		flushWork{from: from, txs: []heldTx{makeHeldTx(7, time.Time{})}, needsReconcile: false},
		flow.Identifier{},
		submitErr,
	)
	assert.True(t, pool.queues[from].nonces.inFlight())

	// The failed in-flight batch still owns the marker: cleared.
	pool.reconcileSubmission(
		flushWork{from: from, txs: []heldTx{makeHeldTx(7, time.Time{})}, needsReconcile: true},
		flow.Identifier{},
		submitErr,
	)
	assert.False(t, pool.queues[from].nonces.inFlight())

	// Unknown EOA: no panic.
	pool.reconcileSubmission(
		flushWork{from: gethCommon.HexToAddress("0xdef"), txs: []heldTx{makeHeldTx(7, time.Time{})}, needsReconcile: true},
		flow.Identifier{},
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

	flowTxID := flow.HexToID(
		"1122334455667788112233445566778811223344556677881122334455667788",
	)

	var logBuf bytes.Buffer
	submitErr := errors.New("network down")
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return submitErr },
		testPoolConfig(),
	)
	// Override with the new-signature closure so we can assert on the flow tx
	// ID in the drop log — the "sending Flow transaction" failure mode (where
	// the tx was built and signed but the AN rejected the send) still carries
	// a valid ID that an operator can look up on Flowscan.
	pool.submitBatch = func(_ context.Context, _ []heldTx) (flow.Identifier, error) {
		return flowTxID, submitErr
	}
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
	assert.Contains(t, out, `"flow_tx_id":"`+flowTxID.Hex()+`"`,
		"wrapping Cadence tx ID must be logged so an operator can trace it on Flowscan",
	)
}

// A successful submission emits a single INFO log carrying eoa, nonce range,
// batch-size, reason and the wrapping Cadence tx ID. Historically split across
// INFO+DEBUG but that duplicated log volume at the default (`debug`) log level;
// consolidated per PR #984 review.
func Test_TxMemPool_SuccessfulSubmitLogsInfoWithAllFields(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	flowTxID := flow.HexToID(
		"aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
	)

	var logBuf bytes.Buffer
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.submitBatch = func(_ context.Context, _ []heldTx) (flow.Identifier, error) {
		return flowTxID, nil
	}
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
	assert.Contains(t, out, `"level":"info"`, "successful send must emit an INFO-level line")
	assert.Contains(t, out, from.Hex(), "eoa must be logged")
	assert.Contains(t, out, `"low-nonce":0`, "low nonce must be logged")
	assert.Contains(t, out, `"high-nonce":1`, "high nonce must be logged")
	assert.Contains(t, out, `"batch-size":2`, "batch size must be logged")
	assert.Contains(t, out, `"reason":"consecutive-prefix"`, "flush reason must be logged")
	assert.Contains(t, out, `"flow_tx_id":"`+flowTxID.Hex()+`"`,
		"wrapping Cadence tx ID must be logged so it can be correlated to Flowscan",
	)
	assert.NotContains(t, out, `"level":"debug"`,
		"successful send must NOT emit a duplicate DEBUG line — consolidated per PR #984 review",
	)
}

// When the Cadence tx build fails before signing, the flow_tx_id is the
// zero identifier — the WARN drop log must OMIT the field rather than emit
// a meaningless all-zero hex string, per Janez's PR #984 review comment.
func Test_TxMemPool_FailedSubmitOmitsFlowTxIDWhenBuildFailed(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)

	var logBuf bytes.Buffer
	submitErr := errors.New("building Flow transaction: signer unavailable")
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return submitErr },
		testPoolConfig(),
	)
	pool.submitBatch = func(_ context.Context, _ []heldTx) (flow.Identifier, error) {
		return flow.Identifier{}, submitErr
	}
	pool.logger = zerolog.New(&logBuf)

	tx0 := signedTestTx(t, key, 0, 1)
	past := time.Now().Add(-time.Second)
	pool.queues[from] = &eoaQueue{
		txs:                    map[uint64]heldTx{0: {txHash: tx0.Hash(), nonce: 0, enqueuedAt: past}},
		collectionWindowEndsAt: past,
		flushDeadline:          past,
	}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	require.Error(t, pool.submitWork(context.Background(), work[0]))

	out := logBuf.String()
	assert.Contains(t, out, `"level":"warn"`, "drop must still be observable at WARN level")
	assert.Contains(t, out, "signer unavailable", "underlying error must be logged")
	assert.NotContains(t, out, `"flow_tx_id"`,
		"flow_tx_id field must be omitted when the ID is zero (build failed pre-signing)",
	)
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

// --- Reconciliation loop tests -------------------------------------------
// These drive reconcileOnce directly (rather than through the background
// goroutine) so behavior can be asserted synchronously and without wall-clock
// sleeps. The 7 cases below map to the recovery spec (behavior spec case 13
// in tx_mempool.go).

// primeReconcilePool sets up a pool with one EOA (`from`) whose fast-path
// submission has succeeded: the nonce tracker records nonce N as consecutively
// submitted, no submission is in flight, and lastFlowTxID / lastSubmittedAt
// are set from the returned values. Returns the flow-tx-id that identifies the
// most-recent wrapper (what reconcileOnce polls) so tests can assert the
// getTxResult callback receives the expected identifier.
func primeReconcilePool(
	t *testing.T,
	pool *TxMemPool,
	clk *fakeClock,
	key *ecdsa.PrivateKey,
	nonce uint64,
	flowTxID flow.Identifier,
) gethCommon.Address {
	t.Helper()
	from := crypto.PubkeyToAddress(key.PublicKey)

	// Fast-path submit sets lastConsecutivelySubmitted, lastFlowTxID, and
	// lastSubmittedAt from a real Add() flow — exercising the actual submission
	// path (rather than seeding fields by hand).
	pool.submitBatch = func(_ context.Context, _ []heldTx) (flow.Identifier, error) {
		return flowTxID, nil
	}
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, nonce, 1)))

	q := pool.queues[from]
	require.NotNil(t, q)
	require.True(t, q.nonces.lastConsecutivelySubmitted.set,
		"precondition: fast-path submission must have advanced lastConsecutivelySubmitted")
	require.Equal(t, nonce, q.nonces.lastConsecutivelySubmitted.v)
	require.Equal(t, flowTxID, q.lastFlowTxID)
	require.Equal(t, clk.now(), q.lastSubmittedAt)
	return from
}

// After a fast-path submission the wrapping Cadence tx can seal with an error
// (e.g. the run.cdc "nonce too high" assertion after intra-block reordering).
// reconcileOnce must observe this and clear the in-flight state so a subsequent
// Add() re-classifies against the on-chain frontier rather than staying wedged
// behind the stale marker.
func Test_TxMemPool_ReconcileClearsMarkerWhenWrapperReverted(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"1111111111111111111111111111111111111111111111111111111111111111",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	var polledID flow.Identifier
	pool.getTxResult = func(_ context.Context, id flow.Identifier) (*flow.TransactionResult, error) {
		polledID = id
		return &flow.TransactionResult{
			Status: flow.TransactionStatusSealed,
			Error:  errors.New("evm_error=nonce too high"),
		}, nil
	}

	pool.reconcileOnce(context.Background())

	assert.Equal(t, flowTxID, polledID, "reconciler must poll the recorded wrapping tx id")

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.False(t, q.nonces.lastConsecutivelySubmitted.set,
		"reverted wrapper must clear lastConsecutivelySubmitted")
	assert.False(t, q.nonces.submitting.set,
		"reverted wrapper must clear submitting")
	assert.Equal(t, flow.Identifier{}, q.lastFlowTxID,
		"reverted wrapper must zero lastFlowTxID so a fresher submission owns the slot")
}

// A wrapper that never seals within TxReconcileStaleAfter is treated as
// dropped: reconcileOnce clears the in-flight marker so the EOA can recover
// without waiting for the idle-queue eviction window.
func Test_TxMemPool_ReconcileClearsMarkerWhenWrapperStale(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"2222222222222222222222222222222222222222222222222222222222222222",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	// Wrapper has not sealed — could be an AN drop or a slow seal. Either way,
	// past the stale threshold reconcileOnce must clear the marker.
	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		return &flow.TransactionResult{Status: flow.TransactionStatusExecuted}, nil
	}

	// Advance past the stale threshold. lastSubmittedAt was stamped at
	// timingClockBase inside primeReconcilePool.
	clk.advance(pool.config.TxReconcileStaleAfter + time.Second)

	pool.reconcileOnce(context.Background())

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.False(t, q.nonces.lastConsecutivelySubmitted.set,
		"stale unsealed wrapper must clear lastConsecutivelySubmitted")
	assert.False(t, q.nonces.submitting.set)
	assert.Equal(t, flow.Identifier{}, q.lastFlowTxID)
}

// A wrapper that sealed cleanly (Status Sealed, no Error) is the healthy path:
// the on-chain nonce has advanced and reconcileOnce must leave the marker
// alone. Resetting here would let a client's retry with the same nonce
// double-spend against the freshly-advanced frontier.
func Test_TxMemPool_ReconcileLeavesMarkerWhenWrapperSealedSuccessfully(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"3333333333333333333333333333333333333333333333333333333333333333",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		return &flow.TransactionResult{Status: flow.TransactionStatusSealed, Error: nil}, nil
	}

	pool.reconcileOnce(context.Background())

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.True(t, q.nonces.lastConsecutivelySubmitted.set,
		"sealed-successful wrapper must NOT clear the marker")
	assert.Equal(t, uint64(3), q.nonces.lastConsecutivelySubmitted.v)
	assert.Equal(t, flowTxID, q.lastFlowTxID,
		"sealed-successful wrapper must preserve lastFlowTxID")
}

func Test_TxMemPool_ReconcileLeavesMarkerWhenWrapperSealedSuccessfullyPastGracePeriod(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"3333333333333333333333333333333333333333333333333333333333333333",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		return &flow.TransactionResult{Status: flow.TransactionStatusSealed, Error: nil}, nil
	}

	// Advance by a sufficient amount — well after the stale threshold.
	clk.advance(pool.config.TxReconcileStaleAfter + 3)

	pool.reconcileOnce(context.Background())

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.True(t, q.nonces.lastConsecutivelySubmitted.set,
		"sealed-successful wrapper must NOT clear the marker")
	assert.Equal(t, uint64(3), q.nonces.lastConsecutivelySubmitted.v)
	assert.Equal(t, flowTxID, q.lastFlowTxID,
		"sealed-successful wrapper must preserve lastFlowTxID")
}

// A wrapper that is still in flight (not yet sealed) within the stale window
// is normal steady-state operation. reconcileOnce must not reset in this case;
// resetting would race the imminent seal and could allow a duplicate submission.
func Test_TxMemPool_ReconcileLeavesMarkerWhenWrapperUnsealedAndFresh(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"4444444444444444444444444444444444444444444444444444444444444444",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		return &flow.TransactionResult{Status: flow.TransactionStatusExecuted}, nil
	}

	// Advance a small amount — well within the stale threshold.
	clk.advance(pool.config.TxReconcileStaleAfter / 3)

	pool.reconcileOnce(context.Background())

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.True(t, q.nonces.lastConsecutivelySubmitted.set,
		"unsealed fresh wrapper must NOT clear the marker")
	assert.Equal(t, uint64(3), q.nonces.lastConsecutivelySubmitted.v)
	assert.Equal(t, flowTxID, q.lastFlowTxID)
}

// An EOA with no outstanding submission marker is not a candidate for
// reconciliation — reconcileOnce must skip it entirely and never issue a
// getTxResult call for it (avoids unnecessary AN traffic on idle EOAs and
// starts up scenarios where every queue is fresh).
func Test_TxMemPool_ReconcileSkipsQueueWithoutMarker(t *testing.T) {
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	from := gethCommon.HexToAddress("0xabc")

	// Empty tracker, no lastFlowTxID: no work for the reconciler.
	pool.queues[from] = &eoaQueue{
		txs:          map[uint64]heldTx{},
		lastActivity: time.Now(),
	}

	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		t.Fatalf("getTxResult must not be called for a queue without an outstanding marker")
		return nil, nil
	}

	pool.reconcileOnce(context.Background())

	// The queue must be untouched.
	q := pool.queues[from]
	require.NotNil(t, q)
	assert.False(t, q.nonces.lastConsecutivelySubmitted.set)
	assert.False(t, q.nonces.submitting.set)
	assert.Equal(t, flow.Identifier{}, q.lastFlowTxID)
}

// While reconcileOnce is polling the AN outside the lock, a concurrent
// submission may advance the EOA's lastFlowTxID to a fresher wrapper. When the
// reconciler re-acquires the lock to reset, it must notice that the flow-tx-id
// has moved on and leave the (now-current) marker alone — otherwise a
// successful just-submitted batch would be wrongly cleared.
func Test_TxMemPool_ReconcileSkipsSupersededFlowTxID(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	originalFlowTxID := flow.HexToID(
		"5555555555555555555555555555555555555555555555555555555555555555",
	)
	newerFlowTxID := flow.HexToID(
		"6666666666666666666666666666666666666666666666666666666666666666",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, originalFlowTxID)

	// The getTxResult call simulates the race: while the reconciler is polling
	// outside the lock, a concurrent successful submission bumps the queue's
	// lastFlowTxID. When the reconciler re-acquires the lock to reset, the
	// stored id will no longer match its snapshot and the reset must be skipped.
	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		pool.queueMux.Lock()
		pool.queues[from].lastFlowTxID = newerFlowTxID
		pool.queueMux.Unlock()
		return &flow.TransactionResult{
			Status: flow.TransactionStatusSealed,
			Error:  errors.New("wrapper reverted"),
		}, nil
	}

	pool.reconcileOnce(context.Background())

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.True(t, q.nonces.lastConsecutivelySubmitted.set,
		"a fresher submission has superseded the snapshot; reconciler must not reset")
	assert.Equal(t, uint64(3), q.nonces.lastConsecutivelySubmitted.v)
	assert.Equal(t, newerFlowTxID, q.lastFlowTxID,
		"the newer lastFlowTxID must survive the reconciler pass")
}

// If a fresh batch enters flight between the reconciler's snapshot and its
// reset — such that lastFlowTxID still matches the snapshot but q.nonces.submitting
// is now set for a newer batch — the reset must be skipped. Otherwise the reset
// would clobber the newer batch's submitting marker and let a client retry
// duplicate a nonce that is legitimately in flight, reintroducing the very
// duplicate-wrapper failure mode this loop exists to prevent.
func Test_TxMemPool_ReconcileSkipsWhenFreshBatchInFlight(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"7777777777777777777777777777777777777777777777777777777777777777",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	// Simulate a concurrent background flush that acquires the lock while the
	// reconciler is out doing its network call, marks a newer batch in flight,
	// but does NOT yet update lastFlowTxID (that happens later in
	// reconcileSubmission on submit success). The reconciler must detect the
	// in-flight marker and skip.
	newerInFlightNonce := uint64(4)
	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		pool.queueMux.Lock()
		pool.queues[from].nonces.markSubmitting(newerInFlightNonce)
		pool.queueMux.Unlock()
		return &flow.TransactionResult{
			Status: flow.TransactionStatusSealed,
			Error:  errors.New("wrapper reverted"),
		}, nil
	}

	pool.reconcileOnce(context.Background())

	q := pool.queues[from]
	require.NotNil(t, q)
	assert.True(t, q.nonces.lastConsecutivelySubmitted.set,
		"snapshot's marker must be preserved; newer batch owns the state now")
	assert.Equal(t, uint64(3), q.nonces.lastConsecutivelySubmitted.v)
	assert.True(t, q.nonces.submitting.set,
		"newer batch's submitting marker must survive the reconciler pass")
	assert.Equal(t, newerInFlightNonce, q.nonces.submitting.v)
	assert.Equal(t, flowTxID, q.lastFlowTxID,
		"lastFlowTxID unchanged (newer batch has not yet ack'd)")
}

// End-to-end: after reconciliation clears a wedged marker, a client's retry
// with the same nonce is accepted (fast-paths) rather than being rejected as
// in flight. This is the operational point of the reconciliation loop.
func Test_TxMemPool_ReconcileClearsAllowsSubsequentAddToBeAccepted(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	clk := newFakeClock(timingClockBase)
	pool := newTestPool(
		// Frontier stays at 3: the reverted wrapper did not advance the chain.
		&fakeNonceProvider{nonce: 3},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	pool.now = clk.now

	flowTxID := flow.HexToID(
		"7777777777777777777777777777777777777777777777777777777777777777",
	)
	from := primeReconcilePool(t, pool, clk, key, 3, flowTxID)

	// While the marker is set, a retry of the same nonce is rejected as in flight.
	err = pool.Add(context.Background(), signedTestTx(t, key, 3, 2))
	require.ErrorIs(t, err, errs.ErrInFlightNonce,
		"before reconciliation, retry with the same nonce must be rejected as in flight")

	// Reconcile with a reverted wrapper: the marker clears.
	pool.getTxResult = func(_ context.Context, _ flow.Identifier) (*flow.TransactionResult, error) {
		return &flow.TransactionResult{
			Status: flow.TransactionStatusSealed,
			Error:  errors.New("evm_error=nonce too high"),
		}, nil
	}
	// Advance past the submission-spacing gap so the subsequent Add can fast-path.
	clk.advance(pool.config.TxSubmissionSpacing + time.Second)
	pool.reconcileOnce(context.Background())

	// After reconciliation, a retry of the same nonce is accepted (fast-paths).
	var retrySubmitted bool
	pool.submitBatch = func(_ context.Context, txs []heldTx) (flow.Identifier, error) {
		retrySubmitted = true
		require.Len(t, txs, 1)
		assert.Equal(t, uint64(3), txs[0].nonce)
		return flow.HexToID("8888888888888888888888888888888888888888888888888888888888888888"), nil
	}
	require.NoError(t, pool.Add(context.Background(), signedTestTx(t, key, 3, 2)),
		"after reconciliation, retry with the same nonce must be accepted")
	assert.True(t, retrySubmitted, "retry must reach the submit path (fast-path)")

	// And the EOA is no longer wedged: the new submission owns lastFlowTxID.
	q := pool.queues[from]
	require.NotNil(t, q)
	assert.NotEqual(t, flowTxID, q.lastFlowTxID,
		"lastFlowTxID must now reference the retry's wrapping tx, not the reverted one")
}
