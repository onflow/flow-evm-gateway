package traces

import (
	"context"
	"sync/atomic"

	"github.com/onflow/flow-go/engine"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ models.Engine = &Engine{}

type Engine struct {
	status            *models.EngineStatus
	blocksBroadcaster *engine.Broadcaster
	blocks            storage.BlockIndexer
	downloader        Downloader
	currentHeight     atomic.Uint64
}

func NewTracesIngestionEngine() *Engine {
	return &Engine{}
}

func (e *Engine) Run(ctx context.Context) error {
	// subscribe to new blocks
	e.blocksBroadcaster.Subscribe(e)

	e.status.MarkReady()
	return nil
}

func (e *Engine) Notify() {
	height := e.currentHeight.Load()

	block, err := e.blocks.GetByHeight(height)
	if err != nil {
		//return nil, err
	}

	for _, h := range block.TransactionHashes {
		trace, err := e.downloader.Download(h.String())
		if err != nil {
			//return nil, err
		}

	}
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
