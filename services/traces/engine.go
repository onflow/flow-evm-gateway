package traces

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/onflow/flow-go-sdk"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-retry"

	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ models.Engine = &Engine{}

// Engine is an implementation of the trace downloader engine.
//
// Traces are ethereum transaction execution traces: https://geth.ethereum.org/docs/developers/evm-tracing
// Currently EVM gateway doesn't produce the traces since it doesn't
// execute the transactions and is thus relying on the execution node
// to produce and upload the traces during execution. This engine
// listens for new transaction events and then downloads and index the
// traces from the transaction execution.
type Engine struct {
	logger          zerolog.Logger
	status          *models.EngineStatus
	blocksPublisher *models.Publisher
	blocks          storage.BlockIndexer
	traces          storage.TraceIndexer
	downloader      Downloader
	collector       metrics.Collector
}

// NewTracesIngestionEngine creates a new instance of the engine.
func NewTracesIngestionEngine(
	blocksPublisher *models.Publisher,
	blocks storage.BlockIndexer,
	traces storage.TraceIndexer,
	downloader Downloader,
	logger zerolog.Logger,
	collector metrics.Collector,
) *Engine {
	return &Engine{
		status:          models.NewEngineStatus(),
		logger:          logger.With().Str("component", "trace-ingestion").Logger(),
		blocksPublisher: blocksPublisher,
		blocks:          blocks,
		traces:          traces,
		downloader:      downloader,
		collector:       collector,
	}
}

func (e *Engine) Run(ctx context.Context) error {
	// subscribe to new blocks
	e.blocksPublisher.Subscribe(e)

	e.status.MarkReady()
	return nil
}

// Notify is a handler that is being used to subscribe for new EVM block notifications.
// This method should be non-blocking.
func (e *Engine) Notify(data any) {
	block, ok := data.(*models.Block)
	if !ok {
		e.logger.Error().Msg("invalid event type sent to trace ingestion")
		return
	}

	// If the block has no transactions, we simply return early
	// as there are no transaction traces to index.
	if len(block.TransactionHashes) == 0 {
		return
	}

	l := e.logger.With().Uint64("evm-height", block.Height).Logger()

	cadenceID, err := e.blocks.GetCadenceID(block.Height)
	if err != nil {
		l.Error().Err(err).Msg("failed to get cadence block ID")
		return
	}

	go e.indexBlockTraces(block, cadenceID)
}

// indexBlockTraces iterates the block transaction hashes and tries to download the traces
func (e *Engine) indexBlockTraces(evmBlock *models.Block, cadenceBlockID flow.Identifier) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	const maxConcurrentDownloads = 5 // limit number of concurrent downloads
	limiter := make(chan struct{}, maxConcurrentDownloads)

	wg := sync.WaitGroup{}

	for _, h := range evmBlock.TransactionHashes {
		wg.Add(1)
		limiter <- struct{}{} // acquire a slot

		go func(h gethCommon.Hash) {
			defer wg.Done()
			defer func() { <-limiter }() // release a slot after done

			l := e.logger.With().
				Str("tx-id", h.String()).
				Str("cadence-block-id", cadenceBlockID.String()).
				Logger()

			err := retry.Fibonacci(ctx, time.Second*1, func(ctx context.Context) error {
				trace, err := e.downloader.Download(h, cadenceBlockID)
				if err != nil {
					l.Warn().Err(err).Msg("retrying failed download")
					return retry.RetryableError(err)
				}

				return e.traces.StoreTransaction(h, trace, nil)
			})

			if err != nil {
				e.collector.TraceDownloadFailed()
				l.Error().Err(err).Msg("failed to download trace")
				return
			}
			l.Info().Msg("trace downloaded successfully")
		}(h)
	}

	wg.Wait()
}

// ID is required by the publisher interface and we return a random uuid since the
// subscription will only happen once by this engine
func (e *Engine) ID() uuid.UUID {
	return uuid.New()
}

// Error is required by the publisher, and we just return a nil,
// since the errors are handled gracefully in the indexBlockTraces
func (e *Engine) Error() <-chan error {
	return nil
}

func (e *Engine) Stop() {
	e.status.MarkStopped()
}

func (e *Engine) Done() <-chan struct{} {
	return e.status.IsDone()
}

func (e *Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}
