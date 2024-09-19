package api

import (
	"context"

	"github.com/goccy/go-json"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/storage"
)

// txTraceResult is the result of a single transaction trace.
type txTraceResult struct {
	TxHash gethCommon.Hash `json:"txHash"`           // transaction hash
	Result interface{}     `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string          `json:"error,omitempty"`  // Trace failure produced by the tracer
}

type DebugAPI struct {
	logger    zerolog.Logger
	tracer    storage.TraceIndexer
	blocks    storage.BlockIndexer
	collector metrics.Collector
}

func NewDebugAPI(tracer storage.TraceIndexer, blocks storage.BlockIndexer, logger zerolog.Logger, collector metrics.Collector) *DebugAPI {
	return &DebugAPI{
		logger:    logger,
		tracer:    tracer,
		blocks:    blocks,
		collector: collector,
	}
}

// TraceTransaction will return a debug execution trace of a transaction if it exists,
// currently we only support CALL traces, so the config is ignored.
func (d *DebugAPI) TraceTransaction(
	ctx context.Context,
	hash gethCommon.Hash,
	_ *tracers.TraceConfig,
) (json.RawMessage, error) {
	res, err := d.tracer.GetTransaction(hash)
	if err != nil {
		return handleError[json.RawMessage](err, d.logger, d.collector)
	}
	return res, nil
}

func (d *DebugAPI) TraceBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	_ *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByHeight(uint64(number.Int64()))
	if err != nil {
		return handleError[[]*txTraceResult](err, d.logger, d.collector)
	}

	results := make([]*txTraceResult, len(block.TransactionHashes))
	for i, h := range block.TransactionHashes {
		txTrace, err := d.TraceTransaction(ctx, h, nil)
		if err != nil {
			results[i] = &txTraceResult{TxHash: h, Error: err.Error()}
		} else {
			results[i] = &txTraceResult{TxHash: h, Result: txTrace}
		}
	}

	return results, nil
}

func (d *DebugAPI) TraceBlockByHash(
	ctx context.Context,
	hash gethCommon.Hash,
	_ *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByID(hash)
	if err != nil {
		return handleError[[]*txTraceResult](err, d.logger, d.collector)
	}

	results := make([]*txTraceResult, len(block.TransactionHashes))
	for i, h := range block.TransactionHashes {
		txTrace, err := d.TraceTransaction(ctx, h, nil)
		if err != nil {
			results[i] = &txTraceResult{TxHash: h, Error: err.Error()}
		} else {
			results[i] = &txTraceResult{TxHash: h, Result: txTrace}
		}
	}

	return results, nil
}
