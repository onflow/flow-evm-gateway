package metrics

import (
	"time"
)

type NoopCollector struct{}

func NewNoopCollector() *NoopCollector {
	return &NoopCollector{}
}

func (c *NoopCollector) ApiErrorOccurred()                        {}
func (c *NoopCollector) TraceDownloadFailed()                     {}
func (c *NoopCollector) ServerPanicked(error)                     {}
func (c *NoopCollector) EvmBlockHeightUpdated(uint64)             {}
func (c *NoopCollector) EvmAccountCalled(string)                  {}
func (c *NoopCollector) MeasureRequestDuration(time.Time, string) {}
