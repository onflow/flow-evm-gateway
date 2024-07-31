package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type NoopCollector struct{}

func NewNoopCollector() *NoopCollector {
	return &NoopCollector{}
}

func (c *NoopCollector) ApiErrorOccurred()                                   {}
func (c *NoopCollector) EvmBlockIndexed()                                    {}
func (c *NoopCollector) EvmBlockIndex() (uint64, error)                      { return 0, nil }
func (c *NoopCollector) IngestionIndexHealthUpdated(bool)                    {}
func (c *NoopCollector) MeasureRequestDuration(time.Time, prometheus.Labels) {}
