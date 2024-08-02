package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type Collector interface {
	ApiErrorOccurred()
	ServerPanicked(err error)
	EvmBlockHeightUpdated(height uint64)
	MeasureRequestDuration(start time.Time, labels prometheus.Labels)
}

type DefaultCollector struct {
	// TODO: for now we cannot differentiate which api request failed number of times
	apiErrorsCounter     prometheus.Counter
	serverPanicsCounters *prometheus.CounterVec
	evmBlockHeight       prometheus.Gauge
	requestDurations     *prometheus.HistogramVec
}

func NewCollector(logger zerolog.Logger) Collector {
	apiErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "api_errors_total",
		Help: "Total number of errors returned by the endpoint resolvers",
	})

	serverPanicsCounters := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "api_server_panics_total",
		Help: "Total number of panics handled by server",
	}, []string{"error"})

	evmBlockHeight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "evm_block_height",
		Help: "Current EVM block height",
	})

	// TODO: Think of adding 'status_code'
	requestDurations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "api_request_duration_seconds",
		Help:    "Duration of requests made to the endpoint resolvers",
		Buckets: prometheus.DefBuckets,
	},
		[]string{"method"})

	if err := registerMetrics(logger, apiErrors, serverPanicsCounters, evmBlockHeight, requestDurations); err != nil {
		logger.Info().Msg("using noop collector as metric register failed")
		return NewNoopCollector()
	}

	return &DefaultCollector{
		apiErrorsCounter:     apiErrors,
		serverPanicsCounters: serverPanicsCounters,
		evmBlockHeight:       evmBlockHeight,
		requestDurations:     requestDurations,
	}
}

func registerMetrics(logger zerolog.Logger, metrics ...prometheus.Collector) error {
	for _, m := range metrics {
		if err := prometheus.Register(m); err != nil {
			logger.Err(err).Msg("failed to register metric")
			return err
		}
	}

	return nil
}

func (c *DefaultCollector) ApiErrorOccurred() {
	c.apiErrorsCounter.Inc()
}

func (c *DefaultCollector) ServerPanicked(err error) {
	c.serverPanicsCounters.With(prometheus.Labels{"error": err.Error()}).Inc()
}

func (c *DefaultCollector) EvmBlockHeightUpdated(height uint64) {
	c.evmBlockHeight.Set(float64(height))
}

func (c *DefaultCollector) MeasureRequestDuration(start time.Time, labels prometheus.Labels) {
	duration := time.Since(start)
	c.requestDurations.With(labels).Observe(float64(duration))
}
