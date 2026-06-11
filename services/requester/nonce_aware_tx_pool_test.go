package requester

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
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

func (f *fakeNonceProvider) GetNonce(_ gethCommon.Address) (uint64, error) {
	return f.nonce, f.err
}

func newTestPool(
	np NonceProvider,
	submit func(context.Context, []heldTx) error,
	cfg config.Config,
) *NonceAwareTxPool {
	pool := &NonceAwareTxPool{
		SingleTxPool: &SingleTxPool{
			logger:      zerolog.Nop(),
			txPublisher: models.NewPublisher[*gethTypes.Transaction](),
			config:      cfg,
			collector:   metrics.NopCollector,
		},
		nonceProvider: np,
		queues:        make(map[gethCommon.Address]*eoaQueue),
	}
	pool.submitBatch = submit
	return pool
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

func Test_NonceAwarePool_FastPathSubmitsImmediately(t *testing.T) {
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
	assert.True(t, q.hasInFlight)
	assert.Equal(t, uint64(0), q.lastSentNonce)
}

func Test_NonceAwarePool_UnexpectedNonceEnqueues(t *testing.T) {
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
	assert.False(t, q.hasInFlight)
}

func Test_NonceAwarePool_InFlightDuplicateRejected(t *testing.T) {
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

func Test_NonceAwarePool_FailedFlushDoesNotWedgeEOA(t *testing.T) {
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
		windowDeadline: past,
		flushDeadline:  past,
	}

	work := pool.collectDueBatches()
	require.Len(t, work, 1)
	assert.True(t, work[0].inFlight)
	require.Len(t, work[0].txs, 2)

	// State was committed optimistically under the lock.
	q := pool.queues[from]
	require.True(t, q.hasInFlight)
	assert.Equal(t, uint64(1), q.lastSentNonce)

	// The submission fails; submitWork must reconcile the in-flight marker.
	err = pool.submitWork(context.Background(), work[0])
	require.ErrorIs(t, err, submitErr)
	assert.False(t, q.hasInFlight)

	// A resubmission of the failed nonce must NOT be rejected as in flight.
	err = pool.Add(context.Background(), signedTestTx(t, key, 0, 2))
	assert.NotErrorIs(t, err, errs.ErrInFlightNonce)
}

func Test_RollbackInFlight_OnlyClearsMatchingBatch(t *testing.T) {
	pool := newTestPool(
		&fakeNonceProvider{nonce: 0},
		func(_ context.Context, _ []heldTx) error { return nil },
		testPoolConfig(),
	)
	from := gethCommon.HexToAddress("0xabc")
	pool.queues[from] = &eoaQueue{
		txs:           map[uint64]heldTx{},
		hasInFlight:   true,
		lastSentNonce: 7,
	}

	// A different (newer) batch owns the marker: no rollback.
	pool.rollbackInFlight(from, 5)
	assert.True(t, pool.queues[from].hasInFlight)

	// The failed batch still owns the marker: rollback.
	pool.rollbackInFlight(from, 7)
	assert.False(t, pool.queues[from].hasInFlight)

	// Unknown EOA: no panic.
	pool.rollbackInFlight(gethCommon.HexToAddress("0xdef"), 7)
}

func Test_RefreshInFlight(t *testing.T) {
	q := &eoaQueue{hasInFlight: true, lastSentNonce: 3}

	// Index has not advanced past the sent nonce: stays in flight.
	q.refreshInFlight(3)
	assert.True(t, q.hasInFlight)

	// Index advanced past the sent nonce: cleared.
	q.refreshInFlight(4)
	assert.False(t, q.hasInFlight)

	// No-op when nothing is in flight.
	q.refreshInFlight(100)
	assert.False(t, q.hasInFlight)
}
