package metrics

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/services/requester"
)

type IngestionIndexHealthTracker struct {
	collector           Collector
	evm                 *requester.EVM
	logger              zerolog.Logger
	maxHeightDifference uint64
	interval            time.Duration
}

func NewIngestionIndexHealthTracker(logger zerolog.Logger, collector Collector, evm *requester.EVM, maxHeightDiff uint64, interval time.Duration) *IngestionIndexHealthTracker {
	return &IngestionIndexHealthTracker{
		collector:           collector,
		evm:                 evm,
		logger:              logger,
		maxHeightDifference: maxHeightDiff,
		interval:            interval,
	}
}

func (c *IngestionIndexHealthTracker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.logger.Info().Msg("shutting down block height collector")
				return
			case <-ticker.C:
				err := c.updateIndexHealth(ctx)
				if err != nil {
					c.logger.Error().Err(err).Msg("error updating index health")
				}
			}
		}
	}()
}

func (c *IngestionIndexHealthTracker) updateIndexHealth(ctx context.Context) error {
	indexedHeight, err := c.collector.EvmBlockIndex()
	if err != nil {
		c.logger.Error().Err(err).Msg("error getting indexed evm height")
		return err
	}

	expectedHeight, err := c.evm.GetLatestEVMHeight(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("error getting latest evm height from access node")
		return err
	}

	healthy := true
	diff := expectedHeight - indexedHeight
	if diff > c.maxHeightDifference {
		healthy = false
	}

	c.collector.IngestionIndexHealthUpdated(healthy)
	return nil
}
