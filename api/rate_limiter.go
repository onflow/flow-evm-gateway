package api

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter"

	"github.com/ethereum/go-ethereum/rpc"

	"github.com/onflow/flow-evm-gateway/metrics"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

type RateLimiter interface {
	Apply(ctx context.Context, method string) error
}

type DefaultRateLimiter struct {
	limiter   limiter.Store
	collector metrics.Collector
	logger    zerolog.Logger
}

func NewRateLimiter(
	limiter limiter.Store,
	collector metrics.Collector,
	logger zerolog.Logger,
) RateLimiter {
	return DefaultRateLimiter{
		limiter:   limiter,
		collector: collector,
		logger:    logger,
	}
}

// Apply will limit requests with the configured limiter.
// In case the limit is reached, an ErrRateLimit error
// will be returned.
func (rl DefaultRateLimiter) Apply(ctx context.Context, method string) error {
	// Future improvement: implement a leaky bucket with wait times instead of errors.
	// Investigate middleware application for all methods, including websockets.
	// Current go-ethereum server doesn't expose ws connection for inspection
	// don't change this to naive middleware handler, because it won't limit
	// websocket requests.

	remote := rpc.PeerInfoFromContext(ctx).RemoteAddr
	if remote == "" {
		return nil // if no client identifier disable limit
	}

	_, _, _, ok, err := rl.limiter.Take(ctx, remote)
	if err != nil {
		return fmt.Errorf("failed to check rate limit: %w", err)
	}
	if !ok {
		rl.collector.RequestRateLimited(method)
		rl.logger.Debug().Str("origin", remote).Msg("rate limit reached")
		return errs.ErrRateLimit
	}

	return nil
}
