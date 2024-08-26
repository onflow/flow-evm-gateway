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
	"github.com/onflow/flow-evm-gateway/tracing"
)

type DebugAPI struct {
	logger           zerolog.Logger
	tracer           storage.TraceIndexer
	blocks           storage.BlockIndexer
	collector        metrics.Collector
	monitoringTracer tracing.Tracer
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
// currently we only support CALL tracing, so the config is ignored.
func (d *DebugAPI) TraceTransaction(
	ctx context.Context,
	hash gethCommon.Hash,
	_ *tracers.TraceConfig,
) (json.RawMessage, error) {
	_, span := d.monitoringTracer.Start(ctx, "DebugAPI.TraceTransaction()")
	defer span.End()

	res, err := d.tracer.GetTransaction(hash)
	if err != nil {
		return handleError[json.RawMessage](err, d.logger, d.collector, span)
	}
	return res, nil
}

func (d *DebugAPI) TraceBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	_ *tracers.TraceConfig,
) ([]json.RawMessage, error) {
	ctx, span := d.monitoringTracer.Start(ctx, "DebugAPI.TraceBlockByNumber()")
	defer span.End()

	block, err := d.blocks.GetByHeight(uint64(number.Int64()))
	if err != nil {
		return handleError[[]json.RawMessage](err, d.logger, d.collector, span)
	}

	results := make([]json.RawMessage, len(block.TransactionHashes))
	for i, h := range block.TransactionHashes {
		results[i], err = d.TraceTransaction(ctx, h, nil)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (d *DebugAPI) TraceBlockByHash(
	ctx context.Context,
	hash gethCommon.Hash,
	_ *tracers.TraceConfig,
) ([]json.RawMessage, error) {
	ctx, span := d.monitoringTracer.Start(ctx, "DebugAPI.TraceBlockByHash()")
	defer span.End()

	block, err := d.blocks.GetByID(hash)
	if err != nil {
		return handleError[[]json.RawMessage](err, d.logger, d.collector, span)
	}

	results := make([]json.RawMessage, len(block.TransactionHashes))
	for i, h := range block.TransactionHashes {
		results[i], err = d.TraceTransaction(ctx, h, nil)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}
