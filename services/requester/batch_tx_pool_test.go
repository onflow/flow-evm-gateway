package requester

import (
	"testing"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func makePooledTx(nonce uint64) pooledEvmTx {
	return pooledEvmTx{
		txHash:     gethCommon.BytesToHash([]byte{byte(nonce)}),
		nonce:      nonce,
		enqueuedAt: time.Now(),
	}
}

func Test_TxQueue_StaleEntry(t *testing.T) {
	now := time.Now()
	spacing := time.Second

	t.Run("zero-value lastSubmittedAt is stale", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{}}
		assert.True(t, q.staleEntry(now, spacing))
	})

	t.Run("non-empty queue is not stale", func(t *testing.T) {
		q := &txQueue{
			txs:             map[uint64]pooledEvmTx{5: makePooledTx(5)},
			lastSubmittedAt: now.Add(-time.Hour),
		}
		assert.False(t, q.staleEntry(now, spacing))
	})

	t.Run("empty and recently submitted is not stale", func(t *testing.T) {
		q := &txQueue{
			txs:             map[uint64]pooledEvmTx{},
			lastSubmittedAt: now.Add(-spacing),
		}
		assert.False(t, q.staleEntry(now, spacing))
	})

	t.Run("empty and past 2x spacing is stale", func(t *testing.T) {
		q := &txQueue{
			txs:             map[uint64]pooledEvmTx{},
			lastSubmittedAt: now.Add(-2 * spacing),
		}
		assert.True(t, q.staleEntry(now, spacing))
	})
}

func Test_TxQueue_ValidNonce(t *testing.T) {
	t.Run("matches state nonce", func(t *testing.T) {
		q := &txQueue{}
		assert.True(t, q.validNonce(5, 5))
	})

	t.Run("matches lastSubmittedNonce+1 when there is prior activity", func(t *testing.T) {
		q := &txQueue{
			lastSubmittedNonce: 5,
			lastSubmittedAt:    time.Now(),
		}
		assert.True(t, q.validNonce(6, 4))
	})

	t.Run("lastSubmittedNonce+1 without prior activity is rejected", func(t *testing.T) {
		q := &txQueue{lastSubmittedNonce: 0}
		assert.False(t, q.validNonce(1, 4))
	})

	t.Run("arbitrary future nonce is rejected", func(t *testing.T) {
		q := &txQueue{
			lastSubmittedNonce: 5,
			lastSubmittedAt:    time.Now(),
		}
		assert.False(t, q.validNonce(9, 5))
	})
}

func Test_TxQueue_SelectSequentialNonces(t *testing.T) {
	t.Run("empty queue returns empty batch", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{}}
		assert.Empty(t, q.selectSequentialNonces(0))
	})

	t.Run("gap at head returns empty batch and preserves queue", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{
			5: makePooledTx(5),
			6: makePooledTx(6),
		}}
		assert.Empty(t, q.selectSequentialNonces(3))
		assert.Len(t, q.txs, 2, "queue must be untouched when head gap blocks selection")
	})

	t.Run("full consecutive run from stateNonce", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{
			3: makePooledTx(3),
			4: makePooledTx(4),
			5: makePooledTx(5),
		}}
		batch := q.selectSequentialNonces(3)
		assert.Len(t, batch, 3)
		assert.Equal(t, uint64(3), batch[0].nonce)
		assert.Equal(t, uint64(5), batch[2].nonce)
		assert.Empty(t, q.txs, "selected txs must be removed from the queue")
	})

	t.Run("stops at first gap and leaves post-gap txs in queue", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{
			1: makePooledTx(1),
			2: makePooledTx(2),
			4: makePooledTx(4),
			5: makePooledTx(5),
		}}
		batch := q.selectSequentialNonces(1)
		assert.Len(t, batch, 2)
		assert.Equal(t, uint64(1), batch[0].nonce)
		assert.Equal(t, uint64(2), batch[1].nonce)
		_, has4 := q.txs[4]
		_, has5 := q.txs[5]
		assert.True(t, has4)
		assert.True(t, has5)
	})

	t.Run("caps at maxTxBatch", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{}}
		for n := uint64(0); n < 10; n++ {
			q.txs[n] = makePooledTx(n)
		}
		batch := q.selectSequentialNonces(0)
		assert.Len(t, batch, maxTxBatch)
		assert.Equal(t, uint64(maxTxBatch-1), batch[maxTxBatch-1].nonce)
		assert.Len(t, q.txs, 10-maxTxBatch, "unselected txs remain in queue")
	})
}

func Test_TxQueue_PruneTxs(t *testing.T) {
	addr := gethCommon.HexToAddress("0x1")

	t.Run("drops nonces below state nonce", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{
			2: makePooledTx(2),
			3: makePooledTx(3),
			5: makePooledTx(5),
		}}
		q.pruneTxs(addr, 4, zerolog.Nop())
		_, has2 := q.txs[2]
		_, has3 := q.txs[3]
		_, has5 := q.txs[5]
		assert.False(t, has2)
		assert.False(t, has3)
		assert.True(t, has5)
	})

	t.Run("drops nonces past the lookahead cap", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{
			10:                            makePooledTx(10),
			10 + maxNonceLookahead:        makePooledTx(10 + maxNonceLookahead),
			10 + maxNonceLookahead + 1:    makePooledTx(10 + maxNonceLookahead + 1),
			10 + maxNonceLookahead + 1000: makePooledTx(10 + maxNonceLookahead + 1000),
		}}
		q.pruneTxs(addr, 10, zerolog.Nop())
		_, hasEdge := q.txs[10+maxNonceLookahead]
		_, hasOver := q.txs[10+maxNonceLookahead+1]
		_, hasFar := q.txs[10+maxNonceLookahead+1000]
		assert.True(t, hasEdge, "nonce exactly at the cap boundary must be retained")
		assert.False(t, hasOver)
		assert.False(t, hasFar)
	})

	t.Run("drops nonces exceeding queue TTL", func(t *testing.T) {
		q := &txQueue{txs: map[uint64]pooledEvmTx{
			2: makePooledTx(2),
			3: makePooledTx(3),
		}}
		tx4 := makePooledTx(4)
		tx4.enqueuedAt = time.Now().Add(-(maxQueueTTL * 2))
		q.txs[4] = tx4
		q.pruneTxs(addr, 1, zerolog.Nop())
		_, has2 := q.txs[2]
		_, has3 := q.txs[3]
		_, has4 := q.txs[4]
		assert.True(t, has2)
		assert.True(t, has3)
		assert.False(t, has4)
	})
}

func Test_TxQueue_SpacingElapsed(t *testing.T) {
	now := time.Now()
	spacing := 2 * time.Second

	t.Run("zero-value lastSubmittedAt always elapsed", func(t *testing.T) {
		q := &txQueue{}
		assert.True(t, q.spacingElapsed(now, spacing))
	})

	t.Run("within spacing not elapsed", func(t *testing.T) {
		q := &txQueue{lastSubmittedAt: now.Add(-time.Second)}
		assert.False(t, q.spacingElapsed(now, spacing))
	})

	t.Run("at exact spacing is elapsed", func(t *testing.T) {
		q := &txQueue{lastSubmittedAt: now.Add(-spacing)}
		assert.True(t, q.spacingElapsed(now, spacing))
	})
}

// Test_BatchTxPool_EnqueuePreservesFresh asserts the rollback path never
// clobbers a same-nonce entry that a concurrent Add() may have parked while
// the flush loop was off-lock: last-write-wins must remain intact.
func Test_BatchTxPool_EnqueuePreservesFresh(t *testing.T) {
	pool := &BatchTxPool{
		txQueues: map[gethCommon.Address]*txQueue{},
	}
	addr := gethCommon.HexToAddress("0xabc")

	fresh := makePooledTx(3)
	fresh.txHash = gethCommon.BytesToHash([]byte("fresh"))

	q := pool.eoaQueueEntry(addr)
	q.txs[3] = fresh

	stale := makePooledTx(3)
	stale.txHash = gethCommon.BytesToHash([]byte("stale"))
	pool.eoaEnqueueTxs(addr, []pooledEvmTx{stale})

	assert.Equal(t, fresh.txHash, q.txs[3].txHash,
		"eoaEnqueueTxs must not overwrite a fresh same-nonce entry")
}

// Test_BatchTxPool_EnqueueFillsMissingNonces asserts that on rollback we do
// re-queue nonces the queue no longer holds — the retry path exists precisely
// so a failed batch is picked up on the next tick.
func Test_BatchTxPool_EnqueueFillsMissingNonces(t *testing.T) {
	pool := &BatchTxPool{
		txQueues: map[gethCommon.Address]*txQueue{},
	}
	addr := gethCommon.HexToAddress("0xabc")

	pool.eoaEnqueueTxs(addr, []pooledEvmTx{
		makePooledTx(1),
		makePooledTx(2),
		makePooledTx(3),
	})

	q := pool.eoaQueueEntry(addr)
	assert.Len(t, q.txs, 3)
	assert.Equal(t, uint64(1), q.txs[1].nonce)
	assert.Equal(t, uint64(2), q.txs[2].nonce)
	assert.Equal(t, uint64(3), q.txs[3].nonce)
}
