package traces

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-retry"
	"golang.org/x/sync/errgroup"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

// todo add ability to backfill missing traces

var _ models.Engine = &Engine{}

type Engine struct {
	logger            zerolog.Logger
	status            *models.EngineStatus
	blocksBroadcaster *engine.Broadcaster
	blocks            storage.BlockIndexer
	traces            storage.TraceIndexer
	downloader        Downloader
	currentHeight     *atomic.Uint64
}

func NewTracesIngestionEngine(
	initEVMHeight uint64,
	blocksBroadcaster *engine.Broadcaster,
	blocks storage.BlockIndexer,
	traces storage.TraceIndexer,
	downloader Downloader,
	logger zerolog.Logger,
) *Engine {
	height := &atomic.Uint64{}
	height.Store(initEVMHeight)

	return &Engine{
		logger:        logger.With().Str("component", "trace-ingestion").Logger(),
		currentHeight: height,
	}
}

func (e *Engine) Run(ctx context.Context) error {
	// subscribe to new blocks
	e.blocksBroadcaster.Subscribe(e)

	e.status.MarkReady()
	return nil
}

func (e *Engine) Notify() {
	// proceed indexing the next height
	height := e.currentHeight.Add(1)

	l := e.logger.With().Uint64("evm-height", height).Logger()

	block, err := e.blocks.GetByHeight(height)
	if err != nil {
		l.Error().Err(err).Msg("failed to get block")
	}

	cadenceID, err := e.blocks.GetCadenceID(height)
	if err != nil {
		l.Error().Err(err).Msg("failed to get cadence block ID")
	}

	if err := e.indexBlockTraces(block, cadenceID); err != nil {
		l.Error().Err(err).Msg("failed to index traces")
	}
}

func (e *Engine) indexBlockTraces(evmBlock *types.Block, cadenceBlockID flow.Identifier) error {
	g := errgroup.Group{}
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	for _, h := range evmBlock.TransactionHashes {
		g.Go(func() error {
			err := retry.Fibonacci(ctx, time.Second*1, func(ctx context.Context) error {
				trace, err := e.downloader.Download(h, cadenceBlockID)
				if err != nil {
					err = fmt.Errorf("failed to download trace %s: %w", h.String(), err)
					e.logger.Debug().Err(err).Msg("retrying download")
					return retry.RetryableError(err)
				}

				if err = e.traces.StoreTransaction(h, trace); err != nil {
					return fmt.Errorf("failed to store trace %s: %w", h.String(), err)
				}

				return nil
			})
			if err != nil {
				return err
			}

			return nil
		})
	}

	return g.Wait()
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
