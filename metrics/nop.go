package metrics

import (
	"time"
)

type nopCollector struct{}

var _ Collector = &nopCollector{}

var NopCollector = &nopCollector{}

func (c *nopCollector) ApiErrorOccurred()                        {}
func (c *nopCollector) TraceDownloadFailed()                     {}
func (c *nopCollector) ServerPanicked(string)                    {}
func (c *nopCollector) EVMHeightIndexed(uint64)                  {}
func (c *nopCollector) EVMAccountInteraction(string)             {}
func (c *nopCollector) MeasureRequestDuration(time.Time, string) {}
