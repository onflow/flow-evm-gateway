package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type NoopCollector struct{}

func (c *NoopCollector) ApiErrorOccurred()                                   {}
func (c *NoopCollector) ServerPanicked()                                     {}
func (c *NoopCollector) MeasureRequestDuration(time.Time, prometheus.Labels) {}
