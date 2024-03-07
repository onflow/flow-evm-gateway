package models

import (
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"time"
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

var _ Engine = &RestartableEngine{}

func NewRestartableEngine(engine Engine, retries uint, logger zerolog.Logger) *RestartableEngine {
	// build a Fibonacci sequence, we could support more strategies in future
	// we use 0 as first run shouldn't be delayed
	backoff := []time.Duration{0, time.Second, time.Second}
	for i := len(backoff); i < int(retries); i++ {
		backoff = append(backoff, backoff[i-2]+backoff[i-1])
	}
	if int(retries) < len(backoff) {
		backoff = backoff[0:retries]
	}

	logger = logger.With().Str("component", "restartable-engine").Logger()

	return &RestartableEngine{
		engine:  engine,
		backoff: backoff,
		logger:  logger,
	}
}

// RestartableEngine is an engine wrapper that tries to restart
// the engine in case of starting errors.
//
// The strategy of the restarts contains Fibonacci backoff time and
// limited number of retries that can be configured.
// Here are backoff values for different retries provided:
// 1s 1s 2s 3s 5s 8s 13s 21s 34s 55s 1m29s 2m24s 3m53s 6m17s 10m10s 16m27s 26m37s 43m4s 1h9m41s
type RestartableEngine struct {
	logger  zerolog.Logger
	engine  Engine
	backoff []time.Duration
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

func (r *RestartableEngine) Run(ctx context.Context) error {
	var err error
	for i, b := range r.backoff {
		select {
		case <-time.After(b): // wait for the backoff duration
			if b > 0 {
				r.logger.Warn().Msg("restarting the engine now")
			}
		case <-ctx.Done():
			r.logger.Warn().Msg("context cancelled, stopping the engine")
			return ctx.Err()
		}

		err = r.engine.Run(ctx)
		if err == nil {
			// don't restart if no error is returned, normal after stop procedure is done
			return nil
		}
		if !IsRecoverableError(err) {
			r.logger.Error().Err(err).Msg("received unrecoverable error")
			// if error is not recoverable just die
			return err
		}

		r.logger.Error().Err(err).Msg(fmt.Sprintf("received recoverable error, restarting for the %d time after backoff time", i))
	}

	r.logger.Error().Msg("failed to recover and restart the engine, stop retrying")
	// if after retries we still get an error it's time to stop
	return err
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

func (e *EngineStatus) MarkReady() {
	close(e.ready)
}

func (e *EngineStatus) MarkDone() {
	close(e.done)
}

func (e *EngineStatus) MarkStopped() {
	close(e.stop)
}
