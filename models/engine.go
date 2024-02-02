package models

import "context"

// Engine defines a processing unit
type Engine interface {
	// Start the engine with context, errors are not expected.
	Start(ctx context.Context) error
	// Stop the engine.
	Stop()
	// Done signals the engine was stopped.
	Done() <-chan struct{}
	// Ready signals the engine was started.
	Ready() <-chan struct{}
}

var _ Engine = &RestartableEngine{}

// RestartableEngine is an engine wrapper that tries to restart
// the engine in case of starting errors.
//
// The strategy of the restarts contains simple backoff time and
// limited number of retries that can be configured.
type RestartableEngine struct {
	engine Engine
}

func (r *RestartableEngine) Stop() {
	r.engine.Stop()
}

func (r *RestartableEngine) Done() <-chan struct{} {
	return r.engine.Done()
}

func (r *RestartableEngine) Ready() <-chan struct{} {
	return r.engine.Ready()
}

func (r *RestartableEngine) Start(ctx context.Context) error {
	// todo add restart logic
	return r.engine.Start(ctx)
}

type EngineStatus struct {
	done  chan struct{}
	ready chan struct{}
	stop  chan struct{}
}

func NewEngineStatus() *EngineStatus {
	return &EngineStatus{
		done:  make(chan struct{}),
		ready: make(chan struct{}),
		stop:  make(chan struct{}),
	}
}

func (e *EngineStatus) IsReady() <-chan struct{} {
	return e.ready
}

func (e *EngineStatus) IsStopped() <-chan struct{} {
	return e.stop
}

func (e *EngineStatus) IsDone() <-chan struct{} {
	return e.done
}

func (e *EngineStatus) Ready() {
	close(e.ready)
}

func (e *EngineStatus) Done() {
	close(e.done)
}

func (e *EngineStatus) Stop() {
	close(e.stop)
}
