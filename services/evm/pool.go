package evm

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/onflow/flow-go-sdk"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-retry"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

const (
	evmErrorRegex = `evm_error=(.*)\n`
)

// todo this is a simple implementation of the transaction pool that is mostly used
// to track the status of submitted transaction, but transactions will always be submitted
// right away, future improvements can make it so the transactions are collected in the pool
// and after submitted based on different strategies.

type TxPool struct {
	logger      zerolog.Logger
	client      *CrossSporkClient
	pool        *sync.Map
	txPublisher *models.Publisher
	// todo add methods to inspect transaction pool state
}

func NewTxPool(
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher,
	logger zerolog.Logger,
) *TxPool {
	return &TxPool{
		logger:      logger.With().Str("component", "tx-pool").Logger(),
		client:      client,
		txPublisher: transactionsPublisher,
		pool:        &sync.Map{},
	}
}

// Send flow transaction that executes EVM run function which takes in the encoded EVM transaction.
// The flow transaction status is awaited and an error is returned in case of a failure in submission,
// or an EVM validation error.
// Until the flow transaction is sealed the transaction will stay in the transaction pool marked as pending.
func (t *TxPool) Send(
	ctx context.Context,
	flowTx *flow.Transaction,
	evmTx *gethTypes.Transaction,
) error {
	t.txPublisher.Publish(evmTx) // publish pending transaction event

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		return err
	}

	// add to pool and delete after transaction is sealed or errored out
	t.pool.Store(evmTx.Hash(), evmTx)
	defer t.pool.Delete(evmTx.Hash())

	backoff := retry.WithMaxDuration(time.Minute*3, retry.NewFibonacci(time.Millisecond*100))

	return retry.Do(ctx, backoff, func(ctx context.Context) error {
		res, err := t.client.GetTransactionResult(ctx, flowTx.ID())
		if err != nil {
			return fmt.Errorf("failed to retrieve flow transaction result %s: %w", flowTx.ID(), err)
		}
		// retry until transaction is sealed
		if res.Status < flow.TransactionStatusSealed {
			return retry.RetryableError(fmt.Errorf("transaction %s not sealed", flowTx.ID()))
		}

		if res.Error != nil {
			if err, ok := parseInvalidError(res.Error); ok {
				return err
			}

			t.logger.Error().Err(res.Error).
				Str("flow-id", flowTx.ID().String()).
				Str("evm-id", evmTx.Hash().Hex()).
				Msg("flow transaction error")

			// hide specific cause since it's an implementation issue
			return fmt.Errorf("failed to submit flow evm transaction %s", evmTx.Hash())
		}

		return nil
	})
}

// this will extract the evm specific error from the Flow transaction error message
// the run.cdc script panics with the evm specific error as the message which we
// extract and return to the client. Any error returned that is evm specific
// is a validation error due to assert statement in the run.cdc script.
func parseInvalidError(err error) (error, bool) {
	r := regexp.MustCompile(evmErrorRegex)
	matches := r.FindStringSubmatch(err.Error())
	if len(matches) != 2 {
		return nil, false
	}

	return errs.NewFailedTransactionError(matches[1]), true
}
