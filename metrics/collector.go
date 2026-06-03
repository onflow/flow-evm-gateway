package metrics

import (
	"fmt"
	"math/big"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

var apiErrors = prometheus.NewCounter(prometheus.CounterOpts{
	Name: prefixedName("api_errors_total"),
	Help: "Total number of API errors",
})

var serverPanicsCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: prefixedName("api_server_panics_total"),
	Help: "Total number of panics in the API server",
}, []string{"reason"})

var operatorBalance = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: prefixedName("operator_balance"),
	Help: "Flow balance of the EVM gateway operator wallet",
})

var cadenceBlockHeight = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: prefixedName("cadence_block_height"),
	Help: "Current Cadence block height",
})

var evmBlockHeight = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: prefixedName("evm_block_height"),
	Help: "Current EVM block height",
})

var evmBlockIndexedCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: prefixedName("blocks_indexed_total"),
	Help: "Total number of blocks indexed",
})

var evmTxIndexedCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: prefixedName("txs_indexed_total"),
	Help: "Total number of transactions indexed",
})

var evmAccountCallCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: prefixedName("evm_account_interactions_total"),
	Help: "Total number of account interactions",
}, []string{"address"})

// TODO: Think of adding 'status_code'
var requestDurations = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    prefixedName("api_request_duration_seconds"),
	Help:    "Duration of the request made a specific API endpoint",
	Buckets: prometheus.DefBuckets,
}, []string{"method"})

var availableSigningKeys = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: prefixedName("available_signing_keys"),
	Help: "Number of keys available for transaction signing",
})

var gasEstimationIterations = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: prefixedName("gas_estimation_iterations"),
	Help: "Number of iterations taken to estimate the gas of a EVM call/tx",
})

// Time difference between block proposal time and block indexing time
var blockIngestionTime = prometheus.NewHistogram(prometheus.HistogramOpts{
	Name:    prefixedName("block_ingestion_time_seconds"),
	Help:    "Latency from EVM block proposal time to indexing completion (wall-clock duration)",
	Buckets: []float64{.5, 1, 2.5, 5, 10, 15, 20, 30, 45},
})

// EVM block processing time during event ingestion, including transaction replay
// and state validation
var blockProcessTime = prometheus.NewSummary(prometheus.SummaryOpts{
	Name: prefixedName("block_process_time_seconds"),
	Help: "Processing time to fully index an EVM block in the local state index",
})

var requestRateLimitedCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: prefixedName("request_rate_limited"),
	Help: "Total number of rate limits by JSON-RPC method",
}, []string{"method"})

var transactionsDroppedCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: prefixedName("transactions_dropped_total"),
	Help: "Total number of EVM transactions dropped due to service errors",
})

var rateLimitedTransactionsCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: prefixedName("rate_limited_transactions_total"),
	Help: "Total number of rate-limited transactions",
})

var flowTotalSupply = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: prefixedName("flow_total_supply"),
	Help: "Total supply of FLOW tokens in EVM at a given time (in smallest unit, wei)",
})

var metrics = []prometheus.Collector{
	apiErrors,
	serverPanicsCounters,
	cadenceBlockHeight,
	evmBlockHeight,
	evmBlockIndexedCounter,
	evmTxIndexedCounter,
	operatorBalance,
	evmAccountCallCounters,
	requestDurations,
	availableSigningKeys,
	gasEstimationIterations,
	blockIngestionTime,
	blockProcessTime,
	requestRateLimitedCounters,
	transactionsDroppedCounter,
	rateLimitedTransactionsCounter,
	flowTotalSupply,
}

type Collector interface {
	ApiErrorOccurred()
	ServerPanicked(reason string)
	CadenceHeightIndexed(height uint64)
	EVMHeightIndexed(height uint64)
	EVMTransactionIndexed(count int)
	EVMAccountInteraction(address string)
	MeasureRequestDuration(start time.Time, method string)
	OperatorBalance(balance uint64)
	AvailableSigningKeys(count int)
	GasEstimationIterations(count int)
	BlockIngestionTime(blockCreation time.Time)
	BlockProcessTime(start time.Time)
	RequestRateLimited(method string)
	TransactionsDropped(count int)
	TransactionRateLimited()
	FlowTotalSupply(totalSupply *big.Int)
}

var _ Collector = &DefaultCollector{}

type DefaultCollector struct {
	logger zerolog.Logger

	// TODO: for now we cannot differentiate which api request failed number of times
	apiErrorsCounter               prometheus.Counter
	serverPanicsCounters           *prometheus.CounterVec
	cadenceBlockHeight             prometheus.Gauge
	evmBlockHeight                 prometheus.Gauge
	evmBlockIndexedCounter         prometheus.Counter
	evmTxIndexedCounter            prometheus.Counter
	operatorBalance                prometheus.Gauge
	evmAccountCallCounters         *prometheus.CounterVec
	requestDurations               *prometheus.HistogramVec
	availableSigningkeys           prometheus.Gauge
	gasEstimationIterations        prometheus.Gauge
	blockIngestionTime             prometheus.Histogram
	blockProcessTime               prometheus.Summary
	requestRateLimitedCounters     *prometheus.CounterVec
	transactionsDroppedCounter     prometheus.Counter
	rateLimitedTransactionsCounter prometheus.Counter
	flowTotalSupply                prometheus.Gauge
}

func NewCollector(logger zerolog.Logger) Collector {
	if err := registerMetrics(logger, metrics...); err != nil {
		logger.Info().Msg("using noop collector as metric register failed")
		return NopCollector
	}

	return &DefaultCollector{
		logger:                         logger,
		apiErrorsCounter:               apiErrors,
		serverPanicsCounters:           serverPanicsCounters,
		cadenceBlockHeight:             cadenceBlockHeight,
		evmBlockHeight:                 evmBlockHeight,
		evmBlockIndexedCounter:         evmBlockIndexedCounter,
		evmTxIndexedCounter:            evmTxIndexedCounter,
		evmAccountCallCounters:         evmAccountCallCounters,
		requestDurations:               requestDurations,
		operatorBalance:                operatorBalance,
		availableSigningkeys:           availableSigningKeys,
		gasEstimationIterations:        gasEstimationIterations,
		blockIngestionTime:             blockIngestionTime,
		blockProcessTime:               blockProcessTime,
		requestRateLimitedCounters:     requestRateLimitedCounters,
		transactionsDroppedCounter:     transactionsDroppedCounter,
		rateLimitedTransactionsCounter: rateLimitedTransactionsCounter,
		flowTotalSupply:                flowTotalSupply,
	}
}

func registerMetrics(logger zerolog.Logger, metrics ...prometheus.Collector) error {
	for _, m := range metrics {
		// During E2E tests, the EVM GW might be bootstrapped again
		// and again, so we make sure to register the metrics on a
		// clean state.
		prometheus.Unregister(m)
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

func (c *DefaultCollector) ServerPanicked(reason string) {
	c.serverPanicsCounters.With(prometheus.Labels{"reason": reason}).Inc()
}

func (c *DefaultCollector) CadenceHeightIndexed(height uint64) {
	c.cadenceBlockHeight.Set(float64(height))
}

func (c *DefaultCollector) EVMHeightIndexed(height uint64) {
	c.evmBlockHeight.Set(float64(height))
	c.evmBlockIndexedCounter.Inc()
}

func (c *DefaultCollector) EVMTransactionIndexed(count int) {
	c.evmTxIndexedCounter.Add(float64(count))
}

func (c *DefaultCollector) EVMAccountInteraction(address string) {
	c.evmAccountCallCounters.With(prometheus.Labels{"address": address}).Inc()
}

func (c *DefaultCollector) OperatorBalance(balance uint64) {
	c.operatorBalance.Set(float64(balance))
}

func (c *DefaultCollector) MeasureRequestDuration(start time.Time, method string) {
	c.requestDurations.
		With(prometheus.Labels{"method": method}).
		Observe(time.Since(start).Seconds())
}

func (c *DefaultCollector) AvailableSigningKeys(count int) {
	c.availableSigningkeys.Set(float64(count))
}

func (c *DefaultCollector) GasEstimationIterations(count int) {
	c.gasEstimationIterations.Set(float64(count))
}

func (c *DefaultCollector) BlockIngestionTime(blockCreation time.Time) {
	c.blockIngestionTime.
		Observe(time.Since(blockCreation).Seconds())
}

func (c *DefaultCollector) BlockProcessTime(start time.Time) {
	c.blockProcessTime.Observe(time.Since(start).Seconds())
}

func (c *DefaultCollector) RequestRateLimited(method string) {
	c.requestRateLimitedCounters.With(
		prometheus.Labels{
			"method": method,
		},
	).Inc()
}

func (c *DefaultCollector) TransactionsDropped(count int) {
	c.transactionsDroppedCounter.Add(float64(count))
}

func (c *DefaultCollector) TransactionRateLimited() {
	c.rateLimitedTransactionsCounter.Inc()
}

func (c *DefaultCollector) FlowTotalSupply(totalSupply *big.Int) {
	if totalSupply == nil {
		return
	}

	floatTotalSupply, accuracy := totalSupply.Float64()
	if accuracy != big.Exact {
		c.logger.Warn().Msg("precision loss when casting total supply to float")
	}

	c.flowTotalSupply.Set(floatTotalSupply)
}

func prefixedName(name string) string {
	return fmt.Sprintf("evm_gateway_%s", name)
}
