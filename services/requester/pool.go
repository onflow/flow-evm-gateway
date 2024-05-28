package requester

import (
	"context"
	"time"

	"github.com/onflow/flow-go-sdk"
)

type TxPool struct {
	client *CrossSporkClient
	pool   map[flow.Identifier]struct{}
}

func (t *TxPool) Send(ctx context.Context, tx flow.Transaction) (*flow.TransactionResult, error) {
	if err := t.client.SendTransaction(ctx, tx); err != nil {
		return nil, err
	}

	t.pool[tx.ID()] = struct{}{}

	const fetchInterval = time.Millisecond * 500
	ticker := time.NewTicker(fetchInterval)
	select {
	case <-ticker.C:
		res, err := t.client.GetTransactionResult(ctx, tx.ID())
		if err != nil {
			return nil, err
		}
		if res.Status > flow.TransactionStatusPending {
			ticker.Stop()
			delete(t.pool, tx.ID())
			return res, nil
		}
	}

	return nil, nil
}
