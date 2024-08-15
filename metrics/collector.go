package metrics

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/model/flow"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
)

type Collector interface {
	ApiErrorOccurred()
	TraceDownloadFailed()
	ServerPanicked(reason string)
	EVMHeightIndexed(height uint64)
	EVMAccountInteraction(address string)
	MeasureRequestDuration(start time.Time, method string)
	EVMFeesCollected(tx models.Transaction, receipt *models.StorageReceipt)
	FlowFeesCollected(from sdk.Address, txEvents []sdk.Event)
}

var _ Collector = &DefaultCollector{}

type DefaultCollector struct {
	logger                    zerolog.Logger
	apiErrorsCounter          prometheus.Counter
	traceDownloadErrorCounter prometheus.Counter
	serverPanicsCounters      *prometheus.CounterVec
	evmBlockHeight            prometheus.Gauge
	evmAccountCallCounters    *prometheus.CounterVec
	requestDurations          *prometheus.HistogramVec
	evmFees                   *prometheus.GaugeVec
	flowFees                  *prometheus.GaugeVec
}

func NewCollector(logger zerolog.Logger) Collector {
	apiErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Name: prefixedName("api_errors_total"),
		Help: "Total number of API errors",
	})

	traceDownloadErrorCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: prefixedName("trace_download_errors_total"),
		Help: "Total number of trace download errors",
	})

	serverPanicsCounters := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prefixedName("api_server_panics_total"),
		Help: "Total number of panics in the API server",
	}, []string{"reason"})

	evmBlockHeight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prefixedName("evm_block_height"),
		Help: "Current EVM block height",
	})

	evmAccountCallCounters := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prefixedName("evm_account_interactions_total"),
		Help: "Total number of account interactions",
	}, []string{"address"})

	// TODO: Think of adding 'status_code'
	requestDurations := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    prefixedName("api_request_duration_seconds"),
		Help:    "Duration of the request made a specific API endpoint",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	evmFees := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prefixedName("evm_fees_total"),
			Help: "The total amount of fees collected on EVM side in gas",
		}, []string{"account"},
	)

	flowFees := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prefixedName("flow_fees_total"),
			Help: "The total amount of fees collected on Flow side in FLOW",
		}, []string{"account"},
	)

	metrics := []prometheus.Collector{
		apiErrors,
		traceDownloadErrorCounter,
		serverPanicsCounters,
		evmBlockHeight,
		evmAccountCallCounters,
		requestDurations,
		evmFees,
		flowFees,
	}

	if err := registerMetrics(logger, metrics...); err != nil {
		logger.Info().Msg("using noop collector as metric register failed")
		return NopCollector
	}

	return &DefaultCollector{
		logger:                    logger,
		apiErrorsCounter:          apiErrors,
		traceDownloadErrorCounter: traceDownloadErrorCounter,
		serverPanicsCounters:      serverPanicsCounters,
		evmBlockHeight:            evmBlockHeight,
		evmAccountCallCounters:    evmAccountCallCounters,
		requestDurations:          requestDurations,
		evmFees:                   evmFees,
		flowFees:                  flowFees,
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

func (c *DefaultCollector) TraceDownloadFailed() {
	c.traceDownloadErrorCounter.Inc()
}

func (c *DefaultCollector) ServerPanicked(reason string) {
	c.serverPanicsCounters.With(prometheus.Labels{"reason": reason}).Inc()
}

func (c *DefaultCollector) EVMHeightIndexed(height uint64) {
	c.evmBlockHeight.Set(float64(height))
}

func (c *DefaultCollector) EVMAccountInteraction(address string) {
	c.evmAccountCallCounters.With(prometheus.Labels{"address": address}).Inc()

}

func (c *DefaultCollector) MeasureRequestDuration(start time.Time, method string) {
	c.requestDurations.
		With(prometheus.Labels{"method": method}).
		Observe(float64(time.Since(start)))
}

func (c *DefaultCollector) EVMFeesCollected(tx models.Transaction, receipt *models.StorageReceipt) {
	if receipt == nil {
		return
	}

	from, err := tx.From()
	if err != nil {
		return
	}

	gasUsed := receipt.GasUsed
	gasPrice := receipt.EffectiveGasPrice

	gasUsedBigInt := new(big.Int).SetUint64(gasUsed)
	gasBigInt := new(big.Int).Mul(gasUsedBigInt, gasPrice)

	gasFloat64, accuracy := gasBigInt.Float64()
	if accuracy != big.Exact {
		c.logger.Warn().Msg("precision lost when converting gas price to float64 in metrics collector")
		return
	}

	gas := float64(gasUsed) * gasFloat64
	c.evmFees.With(prometheus.Labels{"account": from.String()}).Add(gas)
}

func (c *DefaultCollector) FlowFeesCollected(from sdk.Address, txEvents []sdk.Event) {
	feesDeducted, err := findEvent(events.EventTypeFlowFeesDeducted, txEvents)
	if err != nil {
		c.logger.Debug().Err(err).Msg("fees metric will not be collected as appropriate event was not found")
		return
	}

	feesDeductedPayload, err := events.DecodeFlowFeesDeductedEventPayload(feesDeducted.Value)
	if err != nil {
		c.logger.Debug().Err(err).Msg("failed to decode fees deducted payload")
		return
	}

	c.flowFees.
		With(prometheus.Labels{"account": from.String()}).
		Add(float64(feesDeductedPayload.Amount))
}

func prefixedName(name string) string {
	return fmt.Sprintf("evm_gateway_%s", name)
}

func findEvent(eventType flow.EventType, events []sdk.Event) (sdk.Event, error) {
	for _, event := range events {
		if strings.Contains(event.Type, string(eventType)) {
			return event, nil
		}
	}

	return sdk.Event{}, fmt.Errorf("no event with type %s found", eventType)
}
