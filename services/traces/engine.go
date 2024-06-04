package traces

import (
	"context"

	"github.com/onflow/flow-go/engine"

	"github.com/onflow/flow-evm-gateway/models"
)

var _ models.Engine = &Engine{}

type Engine struct {
	status            *models.EngineStatus
	blocksBroadcaster *engine.Broadcaster
}

func NewTracesIngestionEngine() *Engine {
	return &Engine{}
}

func (e Engine) Run(ctx context.Context) error {
	// subscribe to new blocks
	e.blocksBroadcaster.Subscribe(e)

	e.status.MarkReady()
	return nil
}

func (e Engine) Notify() {

}

func (e Engine) Stop() {
	e.status.MarkStopped()
}

func (e Engine) Done() <-chan struct{} {
	return e.status.IsDone()
}

func (e Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}
