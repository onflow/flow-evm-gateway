package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type Collector interface {
	ApiErrorOccurred()
	RequestTimeMeasured(start time.Time)
}

type DefaultCollector struct {
	apiErrors    prometheus.Counter
	responseTime prometheus.Histogram
}

func NewCollector(logger zerolog.Logger) Collector {
	apiErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "api_errors_total",
		Help: "Total number of errors returned by the endpoint resolvers",
	})

	responseTime := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "api_request_duration",
		Help: "Duration of a request made to the endpoint resolver",
	})

	if err := registerMetrics(logger, apiErrors, responseTime); err != nil {
		logger.Info().Msg("Using noop collector as metric register failed")
		return &NoopCollector{}
	}

	return &DefaultCollector{
		apiErrors:    apiErrors,
		responseTime: responseTime,
	}
}

func registerMetrics(logger zerolog.Logger, metrics ...prometheus.Collector) error {
	for _, m := range metrics {
		if err := prometheus.Register(m); err != nil {
			logger.Err(err).Msg("Failed to register metric")
			return err
		}
	}

	return nil
}

func (c *DefaultCollector) ApiErrorOccurred() {
	c.apiErrors.Inc()
}

func (c *DefaultCollector) RequestTimeMeasured(start time.Time) {
	duration := time.Since(start)
	c.responseTime.Observe(float64(duration))
}
