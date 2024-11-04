package api

import (
	"context"
	"fmt"
	"math/big"
	"slices"

	"github.com/goccy/go-json"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/onflow/go-ethereum/eth/tracers/logger"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/evm"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	flowEVM "github.com/onflow/flow-go/fvm/evm"

	// this import is needed for side-effects, because the
	// tracers.DefaultDirectory is relying on the init function
	_ "github.com/onflow/go-ethereum/eth/tracers/js"
	_ "github.com/onflow/go-ethereum/eth/tracers/native"
)

// txTraceResult is the result of a single transaction trace.
type txTraceResult struct {
	TxHash gethCommon.Hash `json:"txHash"`           // transaction hash
	Result interface{}     `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string          `json:"error,omitempty"`  // Trace failure produced by the tracer
}

type DebugAPI struct {
	store        *pebble.Storage
	logger       zerolog.Logger
	tracer       storage.TraceIndexer
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	config       *config.Config
	collector    metrics.Collector
}

func NewDebugAPI(
	store *pebble.Storage,
	tracer storage.TraceIndexer,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	config *config.Config,
	logger zerolog.Logger,
	collector metrics.Collector,
) *DebugAPI {
	return &DebugAPI{
		store:        store,
		logger:       logger,
		tracer:       tracer,
		blocks:       blocks,
		transactions: transactions,
		receipts:     receipts,
		config:       config,
		collector:    collector,
	}
}

// TraceTransaction will return a debug execution trace of a transaction if it exists,
// currently we only support CALL traces, so the config is ignored.
func (d *DebugAPI) TraceTransaction(
	ctx context.Context,
	hash gethCommon.Hash,
	config *tracers.TraceConfig,
) (json.RawMessage, error) {
	if config != nil {
		if *config.Tracer == replayer.TracerName &&
			slices.Equal(config.TracerConfig, json.RawMessage(replayer.TracerConfig)) {
			trace, err := d.tracer.GetTransaction(hash)
			if err == nil {
				return trace, nil
			}
		}
	}

	receipt, err := d.receipts.GetByTransactionID(hash)
	if err != nil {
		return nil, err
	}

	tracer, err := tracerForReceipt(config, receipt)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(receipt.BlockNumber.Uint64())
	if err != nil {
		return nil, err
	}
	// We need to re-execute the given transaction and all the
	// transactions that precede it in the same block, based on
	// the previous block state, to generate the correct trace.
	previousBlock, err := d.blocks.GetByHeight(block.Height - 1)
	if err != nil {
		return nil, err
	}

	blockExecutor, err := d.executorAtBlock(previousBlock)
	if err != nil {
		return nil, err
	}

	// Re-execute the transactions in the order they appear, for the block
	// that contains the given transaction. We set the tracer only for
	// the given transaction, as we don't need it for the preceding
	// transactions. Once we re-execute the desired transaction, we ignore
	// the rest of the transactions in the block, and simply return the trace
	// result.
	txExecuted := false
	var txTracer *tracers.Tracer
	for _, h := range block.TransactionHashes {
		if txExecuted {
			break
		}

		tx, err := d.transactions.Get(h)
		if err != nil {
			return nil, err
		}

		if h == hash {
			txTracer = tracer
			txExecuted = true
		}

		_, err = blockExecutor.Run(tx, txTracer)
		if err != nil {
			return nil, err
		}
	}

	return txTracer.GetResult()
}

func (d *DebugAPI) TraceBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	config *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByHeight(uint64(number.Int64()))
	if err != nil {
		return nil, err
	}

	results := make([]*txTraceResult, len(block.TransactionHashes))

	if config != nil {
		if *config.Tracer == replayer.TracerName &&
			slices.Equal(config.TracerConfig, json.RawMessage(replayer.TracerConfig)) {
			for i, hash := range block.TransactionHashes {
				trace, err := d.tracer.GetTransaction(hash)

				if err != nil {
					results[i] = &txTraceResult{TxHash: hash, Error: err.Error()}
				} else {
					results[i] = &txTraceResult{TxHash: hash, Result: trace}
				}
			}

			return results, nil
		}
	}

	// We need to re-execute all the transactions from the given block,
	// on top of the previous block state, to generate the correct traces.
	previousBlock, err := d.blocks.GetByHeight(block.Height - 1)
	if err != nil {
		return nil, err
	}

	blockExecutor, err := d.executorAtBlock(previousBlock)
	if err != nil {
		return nil, err
	}

	for i, h := range block.TransactionHashes {
		tx, err := d.transactions.Get(h)
		if err != nil {
			return nil, err
		}

		receipt, err := d.receipts.GetByTransactionID(tx.Hash())
		if err != nil {
			return nil, err
		}

		tracer, err := tracerForReceipt(config, receipt)
		if err != nil {
			return nil, err
		}

		_, err = blockExecutor.Run(tx, tracer)
		if err != nil {
			results[i] = &txTraceResult{TxHash: h, Error: err.Error()}
			continue
		}

		txTrace, err := tracer.GetResult()
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
	config *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByID(hash)
	if err != nil {
		return nil, err
	}

	return d.TraceBlockByNumber(ctx, rpc.BlockNumber(block.Height), config)
}

func (d *DebugAPI) TraceCall(
	ctx context.Context,
	args TransactionArgs,
	blockNrOrHash rpc.BlockNumberOrHash,
	config *tracers.TraceCallConfig,
) (interface{}, error) {
	tx, err := encodeTxFromArgs(args)
	if err != nil {
		return nil, err
	}

	// Default address in case user does not provide one
	from := d.config.Coinbase
	if args.From != nil {
		from = *args.From
	}

	var traceConfig *tracers.TraceConfig
	if config != nil {
		traceConfig = &config.TraceConfig
	}

	tracer, err := tracerForReceipt(traceConfig, nil)
	if err != nil {
		return nil, err
	}

	height, err := d.resolveBlockNumberOrHash(&blockNrOrHash)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	ledger := pebble.NewRegister(d.store, block.Height, nil)
	blocksProvider := replayer.NewBlocksProvider(
		d.blocks,
		d.config.FlowNetworkID,
		tracer,
	)
	viewProvider := query.NewViewProvider(
		d.config.FlowNetworkID,
		flowEVM.StorageAccountAddress(d.config.FlowNetworkID),
		ledger,
		blocksProvider,
		120_000_000,
	)

	view, err := viewProvider.GetBlockView(block.Height)
	if err != nil {
		return nil, err
	}

	to := gethCommon.Address{}
	if tx.To != nil {
		to = *tx.To
	}
	opts := []query.DryCallOption{}
	opts = append(opts, query.WithTracer(tracer))
	if config.StateOverrides != nil {
		for addr, overrideAccount := range *config.StateOverrides {
			if overrideAccount.Nonce != nil {
				opts = append(opts, query.WithStateOverrideNonce(addr, uint64(*overrideAccount.Nonce)))
			}
			if overrideAccount.Code != nil {
				opts = append(opts, query.WithStateOverrideCode(addr, *overrideAccount.Code))
			}
			if overrideAccount.Balance != nil {
				opts = append(opts, query.WithStateOverrideBalance(addr, (*big.Int)(*overrideAccount.Balance)))
			}
			if overrideAccount.State != nil {
				opts = append(opts, query.WithStateOverrideState(addr, *overrideAccount.State))
			}
			if overrideAccount.StateDiff != nil {
				opts = append(opts, query.WithStateOverrideStateDiff(addr, *overrideAccount.StateDiff))
			}
		}
	}
	_, err = view.DryCall(
		from,
		to,
		tx.Data,
		tx.Value,
		tx.Gas,
		opts...,
	)

	if err != nil {
		return nil, err
	}

	return tracer.GetResult()
}

func (d *DebugAPI) executorAtBlock(block *models.Block) (*evm.BlockExecutor, error) {
	ledger := pebble.NewRegister(d.store, block.Height, d.store.NewBatch())

	return evm.NewBlockExecutor(
		block,
		ledger,
		d.config.FlowNetworkID,
		d.blocks,
		d.receipts,
		d.logger,
	)
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
		executed, err := d.blocks.LatestEVMHeight()
		if err != nil {
			return 0, err
		}
		height = int64(executed)
	}

	return uint64(height), nil
}

func tracerForReceipt(
	config *tracers.TraceConfig,
	receipt *models.Receipt,
) (*tracers.Tracer, error) {
	tracerCtx := &tracers.Context{}
	if receipt != nil {
		tracerCtx = &tracers.Context{
			BlockHash:   receipt.BlockHash,
			BlockNumber: receipt.BlockNumber,
			TxIndex:     int(receipt.TransactionIndex),
			TxHash:      receipt.TxHash,
		}
	}

	if config == nil {
		config = &tracers.TraceConfig{}
	}

	// Default tracer is the struct logger
	if config.Tracer == nil {
		logger := logger.NewStructLogger(config.Config)
		return &tracers.Tracer{
			Hooks:     logger.Hooks(),
			GetResult: logger.GetResult,
			Stop:      logger.Stop,
		}, nil
	}

	return tracers.DefaultDirectory.New(*config.Tracer, tracerCtx, config.TracerConfig)
}
