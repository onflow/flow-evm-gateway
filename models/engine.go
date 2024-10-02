package models

import (
	"context"
)

// Engine defines a processing unit
type Engine interface {
	// Run the engine with context, errors are not expected.
	Run(ctx context.Context) error
	// Stop the engine.
	Stop()
	// Done signals the engine was stopped.
	Done() <-chan struct{}
	// Ready signals the engine was started.
	Ready() <-chan struct{}
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

func (e *EngineStatus) Ready() <-chan struct{} {
	return e.ready
}

func (e *EngineStatus) Stopped() <-chan struct{} {
	return e.stop
}

func (e *EngineStatus) Done() <-chan struct{} {
	return e.done
}

func (e *EngineStatus) MarkReady() {
	close(e.ready)
}

func (e *EngineStatus) MarkDone() {
	close(e.done)
}

func (e *EngineStatus) MarkStopped() {
	close(e.stop)
}
