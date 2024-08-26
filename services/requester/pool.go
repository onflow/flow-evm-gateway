package requester

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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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
	tracer      trace.Tracer
	// todo add methods to inspect transaction pool state
}

func NewTxPool(
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher,
	logger zerolog.Logger,
	tracer trace.Tracer,
) *TxPool {
	return &TxPool{
		logger:      logger.With().Str("component", "tx-pool").Logger(),
		client:      client,
		txPublisher: transactionsPublisher,
		pool:        &sync.Map{},
		tracer:      tracer,
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
	ctx, span := t.tracer.Start(ctx, "TxPool.Send()")
	defer span.End()

	t.publishTransaction(ctx, evmTx)

	if err := t.sendTransaction(ctx, flowTx); err != nil {
		return err
	}

	if err := t.storeTransaction(ctx, evmTx); err != nil {
		return err
	}

	return t.retryGetTransactionResult(ctx, flowTx, evmTx)
}

func (t *TxPool) sendTransaction(ctx context.Context, flowTx *flow.Transaction) error {
	ctx, span := t.tracer.Start(ctx, "client.sendTransaction()")
	defer span.End()

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	return nil
}

func (t *TxPool) publishTransaction(ctx context.Context, evmTx *gethTypes.Transaction) {
	ctx, span := t.tracer.Start(ctx, "txPublisher.publishTransaction()")
	defer span.End()
	t.txPublisher.Publish(evmTx) // publish pending transaction event
}

func (t *TxPool) storeTransaction(ctx context.Context, evmTx *gethTypes.Transaction) error {
	ctx, span := t.tracer.Start(ctx, "pool.storeTransaction()")
	defer span.End()

	t.pool.Store(evmTx.Hash(), evmTx)
	defer t.pool.Delete(evmTx.Hash())

	return nil
}

func (t *TxPool) retryGetTransactionResult(ctx context.Context, flowTx *flow.Transaction, evmTx *gethTypes.Transaction) error {
	backoff := retry.WithMaxDuration(time.Minute*3, retry.NewFibonacci(time.Millisecond*100))

	return retry.Do(ctx, backoff, func(ctx context.Context) error {
		ctx, span := t.tracer.Start(ctx, "(retryable) client.GetTransactionResult()")
		defer span.End()

		res, err := t.client.GetTransactionResult(ctx, flowTx.ID())
		if err != nil {
			err = fmt.Errorf("failed to retrieve flow transaction result %s: %w", flowTx.ID(), err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if res.Status < flow.TransactionStatusSealed {
			return retry.RetryableError(fmt.Errorf("transaction %s not sealed", flowTx.ID()))
		}

		if res.Error != nil {
			if err, ok := parseInvalidError(res.Error); ok {
				span.SetStatus(codes.Error, err.Error())
				return err
			}

			t.logger.Error().Err(res.Error).
				Str("flow-id", flowTx.ID().String()).
				Str("evm-id", evmTx.Hash().Hex()).
				Msg("flow transaction error")

			err = fmt.Errorf("failed to submit flow evm transaction %s", evmTx.Hash())
			span.SetStatus(codes.Error, err.Error())
			return err
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
