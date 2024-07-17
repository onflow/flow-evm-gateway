package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Collector interface {
	ApiErrorOccurred()
	RequestTimeMeasured(start time.Time)
}

type DefaultCollector struct {
	apiErrors    prometheus.Counter
	responseTime prometheus.Histogram
}

func NewCollector() Collector {
	apiErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "api_errors_total",
		Help: "Total number of errors returned by the endpoint resolvers",
	})

	responseTime := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "api_request_duration",
		Help: "Duration of a request made to the endpoint resolver",
	})

	registerMetrics(apiErrors, responseTime)

	return &DefaultCollector{
		apiErrors:    apiErrors,
		responseTime: responseTime,
	}
}

func registerMetrics(metrics ...prometheus.Collector) {
	for _, m := range metrics {
		prometheus.MustRegister(m)
	}
}

func (c *DefaultCollector) ApiErrorOccurred() {
	c.apiErrors.Inc()
}

func (c *DefaultCollector) RequestTimeMeasured(start time.Time) {
	duration := time.Since(start)
	c.responseTime.Observe(float64(duration))
}
