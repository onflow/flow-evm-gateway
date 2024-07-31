package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type Collector interface {
	ApiErrorOccurred()
	ServerPanicked()
	MeasureRequestDuration(start time.Time, labels prometheus.Labels)
}

type DefaultCollector struct {
	// TODO: for now we cannot differentiate which api request failed number of times
	apiErrorsCounter    prometheus.Counter
	serverPanicsCounter prometheus.Counter
	requestDurations    *prometheus.HistogramVec
}

func NewCollector(logger zerolog.Logger) Collector {
	apiErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "api_errors_total",
		Help: "Total number of errors returned by the endpoint resolvers",
	})

	serverPanicsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "server_panics_total",
		Help: "Total number of panics handled by server",
	})

	// TODO: Think of adding 'status_code'
	requestDurations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "api_request_duration_seconds",
		Help:    "Duration of requests made to the endpoint resolvers",
		Buckets: prometheus.DefBuckets,
	},
		[]string{"method"})

	if err := registerMetrics(logger, apiErrors, serverPanicsCounter, requestDurations); err != nil {
		logger.Info().Msg("Using noop collector as metric register failed")
		return &NoopCollector{}
	}

	return &DefaultCollector{
		apiErrorsCounter:    apiErrors,
		serverPanicsCounter: serverPanicsCounter,
		requestDurations:    requestDurations,
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
	c.apiErrorsCounter.Inc()
}

func (c *DefaultCollector) ServerPanicked() {
	c.serverPanicsCounter.Inc()
}

func (c *DefaultCollector) MeasureRequestDuration(start time.Time, labels prometheus.Labels) {
	duration := time.Since(start)
	c.requestDurations.With(labels).Observe(float64(duration))
}
