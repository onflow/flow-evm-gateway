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

// RestartableEngine is an engine wrapper that tries to restart
// the engine in case of starting errors.
//
// The strategy of the restarts contains simple backoff time and
// limited number of retries that can be configured.
type RestartableEngine struct {
	engine Engine
}
