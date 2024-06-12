package requester

import (
	"context"
	"fmt"
	"time"

	"github.com/onflow/flow-go-sdk"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
)

// todo this is a simple implementation of the transaction pool that is mostly used
// to track the status of submitted transaction, but transactions will always be submitted
// right away, future improvements can make it so the transactions are collected in the pool
// and after submitted based on different strategies.

type TxPool struct {
	client *CrossSporkClient
	pool   map[gethCommon.Hash]gethTypes.Transaction
}

func (t *TxPool) Send(ctx context.Context, flowTx flow.Transaction, evmTx gethTypes.Transaction) (*flow.TransactionResult, error) {
	if err := t.client.SendTransaction(ctx, flowTx); err != nil {
		return nil, err
	}

	// add to pool
	t.pool[evmTx.Hash()] = evmTx

	const fetchInterval = time.Millisecond * 500
	const fetchTimeout = time.Minute * 3
	ticker := time.NewTicker(fetchInterval)
	timeout := time.NewTimer(fetchTimeout)

	defer func() {
		delete(t.pool, evmTx.Hash())
		timeout.Stop()
		ticker.Stop()
	}()

	select {
	case <-ticker.C:
		res, err := t.client.GetTransactionResult(ctx, flowTx.ID())
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve flow transaction result %s: %w", flowTx.ID(), err)
		}
		if res.Status > flow.TransactionStatusPending {
			return res, nil
		}
	case <-timeout.C:
		return nil, fmt.Errorf("failed to retrieve evm transaction result: %s, flow id: %s", evmTx.Hash(), flowTx.ID())
	}

	return nil, nil
}
