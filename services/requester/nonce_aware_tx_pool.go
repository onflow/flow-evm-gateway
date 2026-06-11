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
