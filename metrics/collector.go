package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
)

type Collector interface {
	ApiErrorOccurred()
	EvmBlockIndexed()
	EvmBlockIndex() (uint64, error)
	IngestionIndexHealthUpdated(healthy bool)
	MeasureRequestDuration(start time.Time, labels prometheus.Labels)
}

type DefaultCollector struct {
	// TODO: for now we cannot differentiate which api request failed number of times
	apiErrorsCounter     prometheus.Counter
	evmBlockHeight       prometheus.Counter
	ingestionIndexHealth prometheus.Gauge
	requestDurations     *prometheus.HistogramVec
}

func NewCollector(logger zerolog.Logger) Collector {
	apiErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "api_errors_total",
		Help: "Total number of errors returned by the endpoint resolvers",
	})

	evmBlockHeight := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "evm_block_height",
		Help: "Current EVM block height",
	})

	ingestionIndexHealth := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ingestion_index_health",
		Help: "Indicates if the ingestion index is healthy",
	})

	// TODO: Think of adding 'status_code'
	requestDurations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "api_request_duration_seconds",
		Help:    "Duration of requests made to the endpoint resolvers",
		Buckets: prometheus.DefBuckets,
	},
		[]string{"method"})

	if err := registerMetrics(logger, apiErrors, evmBlockHeight, ingestionIndexHealth, requestDurations); err != nil {
		logger.Info().Msg("using noop collector as metric register failed")
		return &NoopCollector{}
	}

	return &DefaultCollector{
		apiErrorsCounter:     apiErrors,
		evmBlockHeight:       evmBlockHeight,
		ingestionIndexHealth: ingestionIndexHealth,
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

func (c *DefaultCollector) EvmBlockIndexed() {
	c.evmBlockHeight.Inc()
}

func (c *DefaultCollector) EvmBlockIndex() (uint64, error) {
	height := &dto.Metric{}
	if err := c.evmBlockHeight.Write(height); err != nil {
		return 0, err
	}

	return uint64(height.Counter.GetValue()), nil
}

func (c *DefaultCollector) IngestionIndexHealthUpdated(healthy bool) {
	if healthy {
		c.ingestionIndexHealth.Set(1)
	} else {
		c.ingestionIndexHealth.Set(0)
	}
}

func (c *DefaultCollector) MeasureRequestDuration(start time.Time, labels prometheus.Labels) {
	duration := time.Since(start)
	c.requestDurations.With(labels).Observe(float64(duration))
}
