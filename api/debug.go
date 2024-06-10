package api

import (
	"context"

	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/storage"
)

type DebugAPI struct {
	logger zerolog.Logger
	tracer storage.TraceIndexer
}

// TraceTransaction will return a debug execution trace of a transaction if it exists,
// currently we only support CALL traces, so the config is ignored.
func (d *DebugAPI) TraceTransaction(
	ctx context.Context,
	hash gethCommon.Hash,
	_ *tracers.TraceConfig,
) (interface{}, error) {
	res, err := d.tracer.GetTransaction(hash)
	if err != nil {
		return handleError[any](d.logger, err)
	}
	return res, nil
}
