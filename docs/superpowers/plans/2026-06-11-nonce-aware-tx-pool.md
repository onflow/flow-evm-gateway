# Nonce-Aware Transaction Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third TxPool strategy ("nonce-aware mini pool") that submits expected-nonce transactions immediately (zero added latency), holds out-of-order transactions until their nonce gap fills, and spaces consecutive Cadence submissions per EOA to avoid Collection Node reordering — per the design agreed on PR #965 (comments 4675121372 and 4684840320).

**Architecture:** A new `NonceAwareTxPool` embeds `*SingleTxPool` (same pattern as `BatchTxPool`) and adds: a per-EOA queue keyed by nonce, a sliding collection window (300ms, reset per arrival), a submission-spacing guard (1200ms, doubles as the flush deadline — there is deliberately NO separate hard-cap knob), a fast path for expected-nonce txs with nothing in flight, TTL-based expiry (submit anyway so failures are real, not silent), and in-flight nonce dedup. Expected nonces come from the local state index via a new narrow `NonceProvider` adapter wired in bootstrap.

**Tech Stack:** Go, zerolog, cadence, flow-go-sdk, flow-go `fvm/evm/offchain/query`, existing emulator-based e2e test harness in `tests/`.

**Design source of truth:** PR #965 discussion. Key parameters: collection window 300ms; submission spacing 1200ms (1.5x block rate, per Ardit) which ALSO serves as the flush deadline anchored at first enqueue; TTL 30s; max batch size 5 (production default; e2e tests use 10). Constraint validated at startup: window <= spacing. Strategy requires `--tx-state-validation=local-index` and is mutually exclusive with `--tx-batch-mode`.

---

## File Structure

- Create: `services/requester/nonce_provider.go` — `NonceProvider` interface + `LocalNonceProvider` (local index lookup)
- Create: `services/requester/nonce_aware_tx_pool.go` — pool implementation + pure selection helpers
- Create: `services/requester/nonce_aware_tx_pool_test.go` — unit tests for the pure helpers
- Create: `tests/nonce_aware_tx_pool_test.go` — e2e tests against the emulator
- Modify: `config/config.go` — 5 new config fields
- Modify: `cmd/run/cmd.go` — 5 new flags + validation block
- Modify: `models/errors/errors.go` — `ErrInFlightNonce`
- Modify: `bootstrap/bootstrap.go:244-270` — third strategy branch in pool selection

Existing `SingleTxPool` / `BatchTxPool` are NOT modified.

---

### Task 1: Config fields and flags

**Files:**
- Modify: `config/config.go` (after the `EOAActivityCacheTTL` field, ~line 129)
- Modify: `cmd/run/cmd.go` (flags ~line 293, validation ~line 228)

- [ ] **Step 1: Add config fields**

In `config/config.go`, immediately after the `EOAActivityCacheTTL time.Duration` field, add:

```go
	// TxNonceAwareMode configures the gateway to use the nonce-aware transaction
	// pool: transactions carrying the expected next nonce (with nothing in flight)
	// are submitted immediately, out-of-order transactions are held until their
	// nonce gap fills, and consecutive Cadence submissions for the same EOA are
	// spaced apart to avoid Collection Node re-ordering.
	TxNonceAwareMode bool
	// TxCollectionWindow is the per-EOA sliding collection window used by the
	// nonce-aware tx pool. The window resets on each new transaction arrival
	// from the same EOA; when it elapses the collected transactions are flushed.
	TxCollectionWindow time.Duration
	// TxSubmissionSpacing is the minimum gap between two consecutive Cadence
	// transaction submissions for the same EOA (recommended ~1.5x the block
	// production rate). It also serves as the flush deadline for a
	// continuously-fed collection window, anchored at first enqueue.
	TxSubmissionSpacing time.Duration
	// TxPoolTTL is how long the nonce-aware tx pool holds an out-of-order
	// transaction waiting for its nonce gap to fill. On expiry the transaction
	// is submitted anyway, so the failure is observable instead of a silent drop.
	TxPoolTTL time.Duration
	// TxMaxBatchSize is the maximum number of EVM transactions submitted in a
	// single EVM.batchRun Cadence transaction by the nonce-aware tx pool,
	// bounded by the Cadence transaction computation limit.
	TxMaxBatchSize int
```

- [ ] **Step 2: Add flags**

In `cmd/run/cmd.go`, immediately after the `eoa-activity-cache-ttl` flag definition (~line 293), add:

```go
	Cmd.Flags().BoolVar(&cfg.TxNonceAwareMode, "tx-nonce-aware-mode", false, "Enable the nonce-aware transaction pool: expected-nonce transactions are submitted immediately, out-of-order transactions are held until their nonce gap fills. Mutually exclusive with --tx-batch-mode and requires --tx-state-validation=local-index.")
	Cmd.Flags().DurationVar(&cfg.TxCollectionWindow, "tx-collection-window", 300*time.Millisecond, "Per-EOA sliding collection window for the nonce-aware tx pool. Resets on each arrival from the same EOA.")
	Cmd.Flags().DurationVar(&cfg.TxSubmissionSpacing, "tx-submission-spacing", 1200*time.Millisecond, "Minimum gap between consecutive Cadence submissions for the same EOA in the nonce-aware tx pool; also serves as the flush deadline for a continuously-fed collection window. Recommended ~1.5x the block production rate.")
	Cmd.Flags().DurationVar(&cfg.TxPoolTTL, "tx-pool-ttl", 30*time.Second, "How long the nonce-aware tx pool holds an out-of-order transaction waiting for its nonce gap to fill, before submitting it anyway.")
	Cmd.Flags().IntVar(&cfg.TxMaxBatchSize, "tx-max-batch-size", 5, "Maximum number of EVM transactions per EVM.batchRun Cadence transaction in the nonce-aware tx pool.")
```

- [ ] **Step 3: Add validation**

In `cmd/run/cmd.go`, in `parseConfigFromFlags`, immediately after the existing block:

```go
	if cfg.TxBatchMode && cfg.TxStateValidation == config.TxSealValidation {
		return fmt.Errorf("tx-batch-mode should be enabled with tx-state-validation=local-index")
	}
```

add:

```go
	if cfg.TxNonceAwareMode {
		if cfg.TxBatchMode {
			return fmt.Errorf("tx-nonce-aware-mode and tx-batch-mode are mutually exclusive")
		}
		if cfg.TxStateValidation != config.LocalIndexValidation {
			return fmt.Errorf("tx-nonce-aware-mode requires tx-state-validation=local-index")
		}
		if cfg.TxCollectionWindow <= 0 {
			return fmt.Errorf("tx-collection-window must be > 0")
		}
		if cfg.TxSubmissionSpacing <= 0 {
			return fmt.Errorf("tx-submission-spacing must be > 0")
		}
		if cfg.TxCollectionWindow > cfg.TxSubmissionSpacing {
			return fmt.Errorf(
				"tx-collection-window (%s) must not exceed tx-submission-spacing (%s)",
				cfg.TxCollectionWindow, cfg.TxSubmissionSpacing,
			)
		}
		if cfg.TxPoolTTL <= 0 {
			return fmt.Errorf("tx-pool-ttl must be > 0")
		}
		if cfg.TxMaxBatchSize < 1 {
			return fmt.Errorf("tx-max-batch-size must be >= 1")
		}
	}
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success, no output.

- [ ] **Step 5: Commit**

```bash
git add config/config.go cmd/run/cmd.go
git commit -m "feat(config): add nonce-aware tx pool flags and validation"
```

---

### Task 2: NonceProvider — local index nonce lookup

**Files:**
- Create: `services/requester/nonce_provider.go`

Background: `EVM.getBlockView` in `services/requester/requester.go:482-505` shows the exact pattern for reading from the local index — `query.NewViewProvider(chainID, evm.StorageAccountAddress(chainID), registerStore, blocksProvider, blockGasLimit)`. The const `blockGasLimit = 120_000_000` is already defined in `requester.go:44` (same package). Copy the import paths for `query`, `evm`, and the flow-go types exactly as they appear at the top of `requester.go`.

- [ ] **Step 1: Create the file**

```go
package requester

import (
	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	flowGo "github.com/onflow/flow-go/model/flow"

	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

// NonceProvider returns the current nonce of the given EOA address.
// The nonce-aware tx pool uses it to determine the expected next nonce.
type NonceProvider interface {
	GetNonce(address gethCommon.Address) (uint64, error)
}

// LocalNonceProvider reads the EOA nonce from the latest height of the
// local state index.
type LocalNonceProvider struct {
	chainID       flowGo.ChainID
	registerStore *pebble.RegisterStorage
	blocks        storage.BlockIndexer
}

var _ NonceProvider = &LocalNonceProvider{}

func NewLocalNonceProvider(
	chainID flowGo.ChainID,
	registerStore *pebble.RegisterStorage,
	blocks storage.BlockIndexer,
) *LocalNonceProvider {
	return &LocalNonceProvider{
		chainID:       chainID,
		registerStore: registerStore,
		blocks:        blocks,
	}
}

func (p *LocalNonceProvider) GetNonce(address gethCommon.Address) (uint64, error) {
	height, err := p.blocks.LatestEVMHeight()
	if err != nil {
		return 0, err
	}

	viewProvider := query.NewViewProvider(
		p.chainID,
		evm.StorageAccountAddress(p.chainID),
		p.registerStore,
		NewOverridableBlocksProvider(p.blocks, p.chainID, nil),
		blockGasLimit,
	)

	view, err := viewProvider.GetBlockView(height)
	if err != nil {
		return 0, err
	}

	return view.GetNonce(address)
}
```

Note: if any import path or constructor signature differs from what `requester.go` actually uses (e.g. `NewOverridableBlocksProvider` argument order), match `requester.go` — it is the source of truth.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add services/requester/nonce_provider.go
git commit -m "feat(requester): add NonceProvider for local index nonce lookups"
```

---

### Task 3: Pure selection helpers (TDD)

**Files:**
- Create: `services/requester/nonce_aware_tx_pool.go` (types + helpers only in this task)
- Test: `services/requester/nonce_aware_tx_pool_test.go`

- [ ] **Step 1: Write the failing tests**

Create `services/requester/nonce_aware_tx_pool_test.go`:

```go
package requester

import (
	"testing"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./services/requester/ -run 'Test_SelectConsecutivePrefix|Test_SelectExpired' -v`
Expected: compile FAILURE — `heldTx`, `selectConsecutivePrefix`, `selectExpired` undefined.

- [ ] **Step 3: Implement the types and helpers**

Create `services/requester/nonce_aware_tx_pool.go`:

```go
package requester

import (
	"sort"
	"time"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/onflow/cadence"
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./services/requester/ -run 'Test_SelectConsecutivePrefix|Test_SelectExpired' -v`
Expected: PASS (7 subtests).

- [ ] **Step 5: Commit**

```bash
git add services/requester/nonce_aware_tx_pool.go services/requester/nonce_aware_tx_pool_test.go
git commit -m "feat(requester): add nonce selection helpers for nonce-aware pool"
```

---

### Task 4: ErrInFlightNonce

**Files:**
- Modify: `models/errors/errors.go` (next to `ErrDuplicateTransaction`, ~line 34)

- [ ] **Step 1: Add the error**

Immediately after the `ErrDuplicateTransaction` declaration, add:

```go
	// ErrInFlightNonce is returned when a transaction carries a nonce that has
	// already been submitted to the network and is awaiting execution. Letting
	// it through would burn Flow fees on a guaranteed nonce-mismatch failure.
	ErrInFlightNonce = fmt.Errorf("%w: %s", ErrInvalid, "transaction with the same nonce already submitted")
```

(Match the exact declaration style of the surrounding `var` block in that file.)

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add models/errors/errors.go
git commit -m "feat(errors): add ErrInFlightNonce for in-flight nonce dedup"
```

---

### Task 5: NonceAwareTxPool

**Files:**
- Modify: `services/requester/nonce_aware_tx_pool.go` (append to the Task 3 file)

This is the core. Append the following to `services/requester/nonce_aware_tx_pool.go` and extend its import block as needed (`context`, `encoding/hex`, `sync`, `gethTypes "github.com/ethereum/go-ethereum/core/types"`, `"github.com/onflow/flow-go-sdk"` aliased as the file conventions require, `"github.com/rs/zerolog"`, `"github.com/onflow/flow-evm-gateway/config"`, `"github.com/onflow/flow-evm-gateway/metrics"`, `"github.com/onflow/flow-evm-gateway/models"`, `errs "github.com/onflow/flow-evm-gateway/models/errors"`, `"github.com/onflow/flow-evm-gateway/services/requester/keystore"`). Mirror the import style of `batch_tx_pool.go`.

- [ ] **Step 1: Add the pool types and constructor**

```go
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

// NonceAwareTxPool is a `TxPool` implementation that:
//
//   - submits a transaction immediately when it carries the expected next
//     nonce and nothing else is queued or in flight (zero added latency for
//     the common single-tx case);
//   - collects bursts per EOA in a sliding window (reset on each arrival)
//     and submits the longest consecutive nonce prefix as one EVM.batchRun;
//   - spaces consecutive Cadence submissions for the same EOA by
//     TxSubmissionSpacing, so they land in different blocks and cannot be
//     re-ordered by Collection Nodes;
//   - holds out-of-order transactions until their nonce gap fills, the gap
//     is healed by the local index advancing, or TxPoolTTL expires — on
//     expiry the transactions are submitted anyway so the failure is
//     observable on-chain instead of a silent drop;
//   - rejects transactions whose nonce is already in flight, since they
//     would burn Flow fees on a guaranteed nonce-mismatch failure.
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

	pool := &NonceAwareTxPool{
		SingleTxPool:  singleTxPool,
		nonceProvider: nonceProvider,
		queues:        make(map[gethCommon.Address]*eoaQueue),
	}

	go pool.processQueues(ctx)

	return pool, nil
}
```

- [ ] **Step 2: Add Add() — fast path, dedup, enqueue**

```go
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
```

- [ ] **Step 3: Add the state helpers**

```go
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
```

- [ ] **Step 4: Add the flush loop**

```go
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
```

- [ ] **Step 5: Add submitTxBatch**

```go
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
```

- [ ] **Step 6: Build, vet, and run unit tests**

Run: `go build ./... && go vet ./services/requester/ && go test ./services/requester/ -v -run 'Test_Select'`
Expected: build success, vet clean, helper tests still PASS.

- [ ] **Step 7: Commit**

```bash
git add services/requester/nonce_aware_tx_pool.go
git commit -m "feat(requester): add NonceAwareTxPool with fast path, gap handling and spacing"
```

---

### Task 6: Bootstrap wiring

**Files:**
- Modify: `bootstrap/bootstrap.go:244-270` (the pool selection in `StartAPIServer`)

- [ ] **Step 1: Add the third strategy branch**

Replace the existing selection:

```go
	// create transaction pool
	var txPool requester.TxPool
	var err error
	if b.config.TxBatchMode {
```

with:

```go
	// create transaction pool
	var txPool requester.TxPool
	var err error
	if b.config.TxNonceAwareMode {
		nonceProvider := requester.NewLocalNonceProvider(
			b.config.FlowNetworkID,
			b.storages.Registers,
			b.storages.Blocks,
		)
		txPool, err = requester.NewNonceAwareTxPool(
			ctx,
			b.client,
			b.publishers.Transaction,
			b.logger,
			b.config,
			b.collector,
			b.keystore,
			nonceProvider,
		)
	} else if b.config.TxBatchMode {
```

(the `BatchTxPool` and `SingleTxPool` branches stay exactly as they are).

Note: confirm the field names `b.storages.Registers` and `b.storages.Blocks` against the `Storages` struct used elsewhere in `bootstrap.go` (`NewEVM` is called with `b.storages.Registers` a few lines below — match it).

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add bootstrap/bootstrap.go
git commit -m "feat(bootstrap): wire NonceAwareTxPool behind tx-nonce-aware-mode flag"
```

---

### Task 7: E2E tests

**Files:**
- Create: `tests/nonce_aware_tx_pool_test.go`
- Reference (read first, copy patterns exactly): `tests/tx_batching_test.go`, `tests/helpers.go`

**IMPORTANT — before writing any code in this task:** read `tests/tx_batching_test.go` end to end. It contains the exact helpers and assertion patterns these tests must reuse: `setupGatewayNode` (emulator + `bootstrap.CreateMultiKeyAccount` + `bootstrap.Run`), EOA funding, signed-tx creation, `rpcTester.sendRawTx`, balance polling with `assert.Eventually`, and counting `EVM.TransactionExecuted` emulator events between block heights. The code below is complete in intent and structure, but every helper call must match the real signatures in that file — adjust mechanically where they differ, do not redesign.

- [ ] **Step 1: Add the setup helper**

Create `tests/nonce_aware_tx_pool_test.go` with a `setupNonceAwareGatewayNode` that is a copy of `setupGatewayNode` from `tests/tx_batching_test.go` with the config block changed to:

```go
	cfg := config.Config{
		DatabaseDir:         t.TempDir(),
		AccessNodeHost:      grpcHost,
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		FlowNetworkID:      "flow-emulator",
		EVMNetworkID:       types.FlowEVMPreviewNetChainID,
		Coinbase:           eoaTestAccount,
		COAAddress:         *coaAddress,
		COAKey:             privateKey,
		GasPrice:           new(big.Int).SetUint64(0),
		EnforceGasPrice:    true,
		LogLevel:           zerolog.DebugLevel,
		LogWriter:          testLogWriter(),
		TxStateValidation:  config.TxSealValidation,
		TxNonceAwareMode:   true,
		TxCollectionWindow: 300 * time.Millisecond,
		TxSubmissionSpacing: 1200 * time.Millisecond,
		TxPoolTTL:          5 * time.Second,
		TxMaxBatchSize:     10,
	}
```

Rationale recorded as a comment in the file: tests use `TxSealValidation` (like the existing batching tests) so submissions are not racing the index for *validation*; the pool itself still reads the local index for expected nonces, which the ingestion engine populates in the emulator. `TxPoolTTL` is 5s so the TTL test completes quickly. `TxMaxBatchSize` is 10 so a 10-tx burst fits one batch.

- [ ] **Step 2: Test — out-of-order burst all lands**

This is the DFNS regression scenario and the most important test.

```go
func Test_NonceAwarePool_OutOfOrderBurst(t *testing.T) {
	emu, cfg, cancel := setupNonceAwareGatewayNode(t)
	defer cancel()

	// Fund a fresh EOA exactly as Test_TransactionBatchingModeWithConcurrentTxSubmissions does.
	// ... (copy funding + rpcTester setup verbatim from tx_batching_test.go)

	totalTxs := 10
	transferAmount := int64(50_000)

	// Sign totalTxs transactions with sequential nonces 0..9, then send them
	// concurrently in shuffled order.
	signedTxs := make([][]byte, totalTxs)
	for nonce := 0; nonce < totalTxs; nonce++ {
		// build + sign a transfer of transferAmount wei to the receiver,
		// using the same evm signing helper as the batching tests
	}

	order := []int{6, 2, 8, 0, 1, 9, 3, 5, 4, 7}
	var g errgroup.Group
	for _, i := range order {
		signed := signedTxs[i]
		g.Go(func() error {
			_, err := rpcTester.sendRawTx(signed)
			return err
		})
	}
	require.NoError(t, g.Wait())

	// All 10 must execute: assert the receiver balance reaches
	// totalTxs * transferAmount, within a generous window.
	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(receiverAddress)
		return err == nil && balance.Cmp(big.NewInt(int64(totalTxs)*transferAmount)) == 0
	}, time.Second*30, time.Millisecond*200)

	// And exactly 10 EVM.TransactionExecuted events were emitted (no drops,
	// no duplicates) — copy the event-counting block between start/end
	// heights from Test_TransactionBatchingModeWithConcurrentTxSubmissions.
}
```

- [ ] **Step 3: Test — single tx uses the fast path**

```go
func Test_NonceAwarePool_SingleTxImmediateSubmission(t *testing.T) {
	// setup + fund EOA as above

	// Record the emulator's latest block height (start).

	// Send one transfer with nonce 0 and assert the receiver balance updates.
	// Because nonce 0 == expected and nothing is in flight, the pool submits
	// immediately — well before the collection window (300ms) would elapse.

	// Assert exactly one Cadence transaction containing "EVM.run" landed
	// between start and end heights (single-tx batch → run path in run.cdc) —
	// copy the script-inspection assertion pattern from
	// Test_MultipleTransactionSubmissionsWithinSmallInterval (lines ~264-285
	// of tx_batching_test.go).
}
```

- [ ] **Step 4: Test — gap holds, then fills**

```go
func Test_NonceAwarePool_GapHoldAndFill(t *testing.T) {
	// setup + fund EOA as above; sign 5 transfers with nonces 0..4

	// Send nonces 0 and 1; wait until both execute (receiver balance == 2 * amount).

	// Send nonces 3 and 4 (skip 2). Wait 2.5s (past window + spacing) and
	// assert the balance is STILL 2 * amount — the pool is holding 3,4
	// because of the gap at 2.
	time.Sleep(2500 * time.Millisecond)
	// assert balance unchanged

	// Now send nonce 2. Eventually all 5 execute (balance == 5 * amount).
	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(receiverAddress)
		return err == nil && balance.Cmp(big.NewInt(5*transferAmount)) == 0
	}, time.Second*30, time.Millisecond*200)
}
```

- [ ] **Step 5: Test — TTL expiry evicts and submits held txs**

```go
func Test_NonceAwarePool_TTLExpiry(t *testing.T) {
	// setup + fund EOA as above; TxPoolTTL is 5s in the helper config

	// Send a transfer with nonce 5 while the expected nonce is 0. It is held.

	// An immediate identical resubmission is rejected as a duplicate while held:
	_, err = rpcTester.sendRawTx(signedNonce5)
	require.ErrorContains(t, err, "transaction already in pool")

	// The receiver balance must remain 0 while the tx is held (no submission).
	time.Sleep(2 * time.Second)
	// assert balance == 0

	// After TTL + slack, the pool evicts and submits the tx anyway (it fails
	// on-chain with nonce-too-high — observable, not silent). Once evicted,
	// resubmitting the identical raw tx is accepted again (no duplicate error),
	// which proves the eviction happened.
	assert.Eventually(t, func() bool {
		_, err := rpcTester.sendRawTx(signedNonce5)
		return err == nil
	}, time.Second*15, time.Millisecond*500)

	// Balance still 0: nonce 5 can never execute while expected is 0.
	// assert balance == 0
}
```

- [ ] **Step 6: Test — in-flight nonce rejection**

```go
func Test_NonceAwarePool_InFlightNonceRejection(t *testing.T) {
	// setup + fund EOA as above

	// Sign two DIFFERENT transfers both with nonce 0 (e.g. different amounts).

	// Send the first: fast path submits it immediately (in flight).
	// Send the second right away, before the emulator can execute and the
	// index can advance (sub-100ms window; the emulator block time plus
	// ingestion makes this reliable in practice):
	_, err = rpcTester.sendRawTx(secondSignedNonce0)
	require.ErrorContains(t, err, "transaction with the same nonce already submitted")
}
```

- [ ] **Step 7: Run the e2e tests**

Run: `go test ./tests/ -run 'Test_NonceAwarePool' -v -timeout 10m`
Expected: all 5 tests PASS. These spin up real emulators; expect ~1-3 minutes total.

- [ ] **Step 8: Run the full existing test suites to check for regressions**

Run: `go test ./services/... ./models/... ./config/... -count=1` and `go test ./tests/ -run 'Test_TransactionBatching|Test_MultipleTransaction' -v -timeout 15m`
Expected: PASS — the new pool must not affect existing strategies.

- [ ] **Step 9: Commit**

```bash
git add tests/nonce_aware_tx_pool_test.go
git commit -m "test(requester): add e2e tests for nonce-aware tx pool"
```

---

## Self-Review Notes

- Spec coverage check against PR #965 final design: fast path (v2 plan §"Revised Submission Rules" rule 1) → Task 5 Step 2; queue path + window (rule 2) → Task 5 Step 2; expected-nonce computation (rule 3) → Task 5 Step 4; consecutive prefix (rule 4) → Task 3; spacing guard (rule 5) → Task 5 Steps 3-4; in-flight tracking via local index (rule 6 + "On-Chain Feedback") → Task 5 Step 3 (`refreshInFlight`); TTL submit-anyway → Task 5 Step 4; in-flight dedup (Ardit, required) → Task 5 Step 2 + Task 4; 1200ms default = 1.5x block rate (Ardit) → Task 1; merged hard-cap knob (flushDeadline = spacing) → Task 1 (no separate flag) + Task 5 (`flushDeadline`); window <= spacing validation → Task 1 Step 3; max batch size (Cadence limit) → Task 1 + Task 3 cap; sticky sessions → deployment concern, no code; stale-nonce pruning (gap healed externally) → Task 5 Step 4 (`pruneStaleTxs`).
- Known deliberate scope cuts: no per-EOA `time.Timer` map (a 50ms scan ticker is simpler and equally correct at this resolution); no replacement-by-gas-price semantics (Flow has no MEV ordering — same-nonce queued tx is replaced last-write-wins, same-nonce in-flight is rejected); existing `SingleTxPool`/`BatchTxPool` untouched.
- E2E task intentionally directs the implementer to copy helper invocations from `tests/tx_batching_test.go` rather than inventing signatures; the test logic, assertions, and timing values above are normative.
