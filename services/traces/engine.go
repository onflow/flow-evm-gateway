package traces

import (
	"context"
	"sync"
	"time"

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
	*models.EngineStatus

	logger          zerolog.Logger
	blocksPublisher *models.Publisher[*models.Block]
	blocks          storage.BlockIndexer
	traces          storage.TraceIndexer
	downloader      Downloader
	collector       metrics.Collector
}

// NewTracesIngestionEngine creates a new instance of the engine.
func NewTracesIngestionEngine(
	blocksPublisher *models.Publisher[*models.Block],
	blocks storage.BlockIndexer,
	traces storage.TraceIndexer,
	downloader Downloader,
	logger zerolog.Logger,
	collector metrics.Collector,
) *Engine {
	return &Engine{
		EngineStatus: models.NewEngineStatus(),

		logger:          logger.With().Str("component", "trace-ingestion").Logger(),
		blocksPublisher: blocksPublisher,
		blocks:          blocks,
		traces:          traces,
		downloader:      downloader,
		collector:       collector,
	}
}

// Run the engine.
// TODO: use the context to stop the engine.
func (e *Engine) Run(ctx context.Context) error {
	// subscribe to new blocks
	e.blocksPublisher.Subscribe(e)

	e.MarkReady()
	return nil
}

// Notify is a handler that is being used to subscribe for new EVM block notifications.
// This method should be non-blocking.
func (e *Engine) Notify(block *models.Block) {
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

// Error is required by the publisher, and we just return a nil,
// since the errors are handled gracefully in the indexBlockTraces
func (e *Engine) Error() <-chan error {
	return nil
}

func (e *Engine) Stop() {
	e.MarkStopped()
}
