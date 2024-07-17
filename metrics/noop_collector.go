package metrics

import (
	"time"
)

type NoopCollector struct{}

func (c *NoopCollector) ApiErrorOccurred()             {}
func (c *NoopCollector) RequestTimeMeasured(time.Time) {}
