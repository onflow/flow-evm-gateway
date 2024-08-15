package metrics

import (
	"fmt"
	"time"

	"github.com/onflow/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Collector interface {
	ApiErrorOccurred()
	TraceDownloadFailed()
	ServerPanicked(reason string)
	EVMHeightIndexed(height uint64)
	EVMAccountInteraction(address *common.Address)
	MeasureRequestDuration(start time.Time, method string)
}

var _ Collector = &DefaultCollector{}

type DefaultCollector struct {
	apiErrorsCounter          prometheus.Counter
	traceDownloadErrorCounter prometheus.Counter
	serverPanicsCounters      *prometheus.CounterVec
	evmBlockHeight            prometheus.Gauge
	evmAccountCallCounters    *prometheus.CounterVec
	requestDurations          *prometheus.HistogramVec
}

func NewCollector() Collector {
	registry := prometheus.NewRegistry()
	factory := promauto.With(registry)

	apiErrors := factory.NewCounter(prometheus.CounterOpts{
		Name: prefixedName("api_errors_total"),
		Help: "Total number of API errors",
	})

	traceDownloadErrorCounter := factory.NewCounter(prometheus.CounterOpts{
		Name: prefixedName("trace_download_errors_total"),
		Help: "Total number of trace download errors",
	})

	serverPanicsCounters := factory.NewCounterVec(prometheus.CounterOpts{
		Name: prefixedName("api_server_panics_total"),
		Help: "Total number of panics in the API server",
	}, []string{"reason"})

	evmBlockHeight := factory.NewGauge(prometheus.GaugeOpts{
		Name: prefixedName("evm_block_height"),
		Help: "Current EVM block height",
	})

	evmAccountCallCounters := factory.NewCounterVec(prometheus.CounterOpts{
		Name: prefixedName("evm_account_interactions_total"),
		Help: "Total number of account interactions",
	}, []string{"address"})

	requestDurations := factory.NewHistogramVec(prometheus.HistogramOpts{
		Name:    prefixedName("api_request_duration_seconds"),
		Help:    "Duration of the request made a specific API endpoint",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	return &DefaultCollector{
		apiErrorsCounter:          apiErrors,
		traceDownloadErrorCounter: traceDownloadErrorCounter,
		serverPanicsCounters:      serverPanicsCounters,
		evmBlockHeight:            evmBlockHeight,
		evmAccountCallCounters:    evmAccountCallCounters,
		requestDurations:          requestDurations,
	}
}

func (c *DefaultCollector) ApiErrorOccurred() {
	c.apiErrorsCounter.Inc()
}

func (c *DefaultCollector) TraceDownloadFailed() {
	c.traceDownloadErrorCounter.Inc()
}

func (c *DefaultCollector) ServerPanicked(reason string) {
	c.serverPanicsCounters.With(prometheus.Labels{"reason": reason}).Inc()
}

func (c *DefaultCollector) EVMHeightIndexed(height uint64) {
	c.evmBlockHeight.Set(float64(height))
}

func (c *DefaultCollector) EVMAccountInteraction(address *common.Address) {
	if address != nil {
		c.evmAccountCallCounters.With(prometheus.Labels{"address": address.String()}).Inc()
	}
}

func (c *DefaultCollector) MeasureRequestDuration(start time.Time, method string) {
	c.requestDurations.
		With(prometheus.Labels{"method": method}).
		Observe(float64(time.Since(start)))
}

func prefixedName(name string) string {
	return fmt.Sprintf("evm_gateway_%s", name)
}
