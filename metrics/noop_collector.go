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
func (c *NoopCollector) TraceDownloadFailed()                                {}
func (c *NoopCollector) ServerPanicked(error)                                {}
func (c *NoopCollector) EvmAccountCalled(string)                             {}
func (c *NoopCollector) MeasureRequestDuration(time.Time, prometheus.Labels) {}
