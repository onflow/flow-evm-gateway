package api

import (
	"context"
	"fmt"

	"github.com/goccy/go-json"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/state"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

const (
	tracerConfig = `{ "onlyTopCall": true }`
	tracerName   = "callTracer"
)

// txTraceResult is the result of a single transaction trace.
type txTraceResult struct {
	TxHash gethCommon.Hash `json:"txHash"`           // transaction hash
	Result interface{}     `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string          `json:"error,omitempty"`  // Trace failure produced by the tracer
}

type DebugAPI struct {
	logger       zerolog.Logger
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	collector    metrics.Collector
	store        *pebble.Storage
	config       *config.Config
}

func NewDebugAPI(
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	store *pebble.Storage,
	config *config.Config,
	logger zerolog.Logger,
	collector metrics.Collector,
) *DebugAPI {
	return &DebugAPI{
		logger:       logger,
		blocks:       blocks,
		transactions: transactions,
		receipts:     receipts,
		collector:    collector,
		store:        store,
		config:       config,
	}
}

// TraceTransaction will return a debug execution trace of a transaction if it exists,
// currently we only support CALL traces, so the config is ignored.
func (d *DebugAPI) TraceTransaction(
	ctx context.Context,
	hash gethCommon.Hash,
	_ *tracers.TraceConfig,
) (json.RawMessage, error) {
	receipt, err := d.receipts.GetByTransactionID(hash)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(receipt.BlockNumber.Uint64())
	if err != nil {
		return nil, err
	}

	// start a new database batch
	batch := d.store.NewBatch()
	defer func() {
		if err := batch.Close(); err != nil {
			d.logger.Warn().Err(err).Msg("failed to close batch")
		}
	}()

	tracerConfig := json.RawMessage(tracerConfig)
	tracerCtx := &tracers.Context{
		BlockHash:   receipt.BlockHash,
		BlockNumber: receipt.BlockNumber,
		TxIndex:     int(receipt.TransactionIndex),
		TxHash:      receipt.TxHash,
	}
	tracer, err := tracers.DefaultDirectory.New(tracerName, tracerCtx, tracerConfig)
	if err != nil {
		return nil, err
	}

	registerHeight := block.Height - 1

	registers := pebble.NewRegister(d.store, registerHeight, batch)
	state, err := state.NewBlockState(
		block,
		registers,
		d.config.FlowNetworkID,
		d.blocks,
		d.receipts,
		d.logger,
		tracer,
	)
	if err != nil {
		return nil, err
	}

	executed := false
	var txTracer *tracers.Tracer
	for _, h := range block.TransactionHashes {
		if executed {
			break
		}

		tx, err := d.transactions.Get(h)
		if err != nil {
			return nil, err
		}

		if h == hash {
			txTracer = tracer
		}

		_, err = state.ExecuteWithTracer(tx, txTracer)
		if err != nil {
			return nil, err
		}

		if h == hash {
			executed = true
		}
	}

	result, err := tracer.GetResult()

	return result, err
}

func (d *DebugAPI) TraceBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	_ *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByHeight(uint64(number.Int64()))
	if err != nil {
		return nil, err
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
		return nil, err
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

func (d *DebugAPI) TraceCall(
	ctx context.Context,
	args TransactionArgs,
	blockNrOrHash rpc.BlockNumberOrHash,
	config *tracers.TraceCallConfig,
) (interface{}, error) {
	tx, err := encodeTxFromArgs(args)
	if err != nil {
		return handleError[interface{}](err, d.logger, d.collector)
	}

	// Default address in case user does not provide one
	from := d.config.Coinbase
	if args.From != nil {
		from = *args.From
	}

	height, err := d.resolveBlockNumberOrHash(&blockNrOrHash)
	if err != nil {
		return nil, err
	}

	// start a new database batch
	batch := d.store.NewBatch()
	defer func() {
		if err := batch.Close(); err != nil {
			d.logger.Warn().Err(err).Msg("failed to close batch")
		}
	}()

	tracerConfig := json.RawMessage(tracerConfig)
	tracer, err := tracers.DefaultDirectory.New(tracerName, &tracers.Context{}, tracerConfig)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(height)
	if err != nil {
		return handleError[interface{}](err, d.logger, d.collector)
	}

	registers := pebble.NewRegister(d.store, height, batch)
	state, err := state.NewBlockState(
		block,
		registers,
		d.config.FlowNetworkID,
		d.blocks,
		d.receipts,
		d.logger,
		tracer,
	)
	if err != nil {
		return handleError[interface{}](err, d.logger, d.collector)
	}

	_, err = state.Call(from, tx)
	if err != nil {
		return handleError[interface{}](err, d.logger, d.collector)
	}

	return tracer.GetResult()
}

func (d *DebugAPI) resolveBlockNumberOrHash(block *rpc.BlockNumberOrHash) (uint64, error) {
	err := fmt.Errorf("%w: neither block number nor hash specified", errs.ErrInvalid)
	if block == nil {
		return 0, err
	}
	if number, ok := block.Number(); ok {
		return d.resolveBlockNumber(number)
	}

	if hash, ok := block.Hash(); ok {
		evmHeight, err := d.blocks.GetHeightByID(hash)
		if err != nil {
			return 0, err
		}
		return evmHeight, nil
	}

	return 0, err
}

func (d *DebugAPI) resolveBlockNumber(number rpc.BlockNumber) (uint64, error) {
	height := number.Int64()

	// if special values (latest) we return latest executed height
	if height < 0 {
		executed, err := d.blocks.LatestExecutedHeight()
		if err != nil {
			return 0, err
		}
		height = int64(executed)
	}

	return uint64(height), nil
}
