package api

import (
	"context"

	"github.com/onflow/go-ethereum/signer/core"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter"

	errs "github.com/onflow/flow-evm-gateway/api/errors"
)

// rateLimit will limit requests with the provider limiter, in case the limit
// is reached ErrRateLimit error will be returned.
func rateLimit(ctx context.Context, limiter limiter.Store, logger zerolog.Logger) error {
	// Future improvement: implement a leaky bucket with wait times instead of errors.
	// Investigate middleware application for all methods, including websockets.
	// Current go-ethereum server doesn't expose ws connection for inspection
	// don't change this to naive middleware handler, because it won't limit
	// websocket requests.

	remote := core.MetadataFromContext(ctx).Remote
	if remote == "NA" {
		return nil // if no client identifier disable limit
	}

	_, _, _, ok, _ := limiter.Take(ctx, remote)
	if !ok {
		logger.Debug().Str("origin", remote).Msg("rate limit reached")
		return errs.ErrRateLimit
	}

	return nil
}
