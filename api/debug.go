package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/goccy/go-json"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/onflow/go-ethereum/eth/tracers/logger"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/evm"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/services/requester"
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
	registerStore *pebble.RegisterStorage
	logger        zerolog.Logger
	tracer        storage.TraceIndexer
	blocks        storage.BlockIndexer
	transactions  storage.TransactionIndexer
	receipts      storage.ReceiptIndexer
	client        *requester.CrossSporkClient
	config        *config.Config
	collector     metrics.Collector
}

func NewDebugAPI(
	registerStore *pebble.RegisterStorage,
	tracer storage.TraceIndexer,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	client *requester.CrossSporkClient,
	config *config.Config,
	logger zerolog.Logger,
	collector metrics.Collector,
) *DebugAPI {
	return &DebugAPI{
		registerStore: registerStore,
		logger:        logger,
		tracer:        tracer,
		blocks:        blocks,
		transactions:  transactions,
		receipts:      receipts,
		client:        client,
		config:        config,
		collector:     collector,
	}
}

// TraceTransaction will return a debug execution trace of a transaction, if it exists.
func (d *DebugAPI) TraceTransaction(
	_ context.Context,
	hash gethCommon.Hash,
	config *tracers.TraceConfig,
) (json.RawMessage, error) {
	// If the given trace config is equal to the default call tracer used
	// in block replay during ingestion, then we fetch the trace result
	// from the Traces DB.
	if isDefaultCallTracer(config) {
		trace, err := d.tracer.GetTransaction(hash)
		// If there is no error, we return the trace result from the DB.
		if err == nil {
			return trace, nil
		}

		// If we got an error of `ErrEntityNotFound`, for whatever reason,
		// we simply re-compute the trace below. If we got any other error,
		// we return it.
		if !errors.Is(err, errs.ErrEntityNotFound) {
			d.logger.Error().Err(err).Msgf(
				"failed to retrieve default call trace for tx: %s",
				hash,
			)
			return nil, err
		}
	}

	receipt, err := d.receipts.GetByTransactionID(hash)
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

	tracer, err := tracerForReceipt(config, receipt)
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

		if err = blockExecutor.Run(tx, txTracer); err != nil {
			return nil, err
		}
	}

	if txTracer != nil {
		return txTracer.GetResult()
	}

	return nil, fmt.Errorf("failed to trace transaction with hash: %s", hash)
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

	// If the given trace config is equal to the default call tracer used
	// in block replay during ingestion, then we fetch the trace result
	// from the Traces DB.
	if isDefaultCallTracer(config) {
		for i, hash := range block.TransactionHashes {
			trace, err := d.TraceTransaction(ctx, hash, config)

			if err != nil {
				results[i] = &txTraceResult{TxHash: hash, Error: err.Error()}
			} else {
				results[i] = &txTraceResult{TxHash: hash, Result: trace}
			}
		}

		return results, nil
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

		if err = blockExecutor.Run(tx, tracer); err != nil {
			results[i] = &txTraceResult{TxHash: h, Error: err.Error()}
		} else if txTrace, err := tracer.GetResult(); err != nil {
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
	_ context.Context,
	args ethTypes.TransactionArgs,
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

	if config == nil {
		config = &tracers.TraceCallConfig{}
	}

	tracer, err := tracerForReceipt(&config.TraceConfig, nil)
	if err != nil {
		return nil, err
	}

	height, err := resolveBlockTag(&blockNrOrHash, d.blocks, d.logger)
	if err != nil {
		return nil, err
	}

	cdcHeight, err := d.blocks.GetCadenceHeight(height)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	blocksProvider := replayer.NewBlocksProvider(
		d.blocks,
		d.config.FlowNetworkID,
		tracer,
	)
	viewProvider := query.NewViewProvider(
		d.config.FlowNetworkID,
		flowEVM.StorageAccountAddress(d.config.FlowNetworkID),
		d.registerStore,
		blocksProvider,
		BlockGasLimit,
	)

	view, err := viewProvider.GetBlockView(block.Height)
	if err != nil {
		return nil, err
	}

	to := gethCommon.Address{}
	if tx.To != nil {
		to = *tx.To
	}
	rca := requester.NewRemoteCadenceArch(cdcHeight, d.client, d.config.FlowNetworkID)

	opts := []query.DryCallOption{}
	opts = append(opts, query.WithTracer(tracer))
	opts = append(opts, query.WithExtraPrecompiledContracts([]types.PrecompiledContract{rca}))
	if config.StateOverrides != nil {
		for addr, account := range *config.StateOverrides {
			// Override account nonce.
			if account.Nonce != nil {
				opts = append(opts, query.WithStateOverrideNonce(addr, uint64(*account.Nonce)))
			}
			// Override account(contract) code.
			if account.Code != nil {
				opts = append(opts, query.WithStateOverrideCode(addr, *account.Code))
			}
			// Override account balance.
			if account.Balance != nil {
				opts = append(opts, query.WithStateOverrideBalance(addr, (*big.Int)(*account.Balance)))
			}
			if account.State != nil && account.StateDiff != nil {
				return nil, fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
			}
			// Replace entire state if caller requires.
			if account.State != nil {
				opts = append(opts, query.WithStateOverrideState(addr, *account.State))
			}
			// Apply state diff into specified accounts.
			if account.StateDiff != nil {
				opts = append(opts, query.WithStateOverrideStateDiff(addr, *account.StateDiff))
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
	snapshot, err := d.registerStore.GetSnapshotAt(block.Height)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get register snapshot at block height %d: %w",
			block.Height,
			err,
		)
	}
	ledger := storage.NewRegisterDelta(snapshot)

	return evm.NewBlockExecutor(
		block,
		ledger,
		d.config.FlowNetworkID,
		d.blocks,
		d.receipts,
		d.logger,
	), nil
}

func tracerForReceipt(
	config *tracers.TraceConfig,
	receipt *models.Receipt,
) (*tracers.Tracer, error) {
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

	tracerCtx := &tracers.Context{}
	if receipt != nil {
		tracerCtx = &tracers.Context{
			BlockHash:   receipt.BlockHash,
			BlockNumber: receipt.BlockNumber,
			TxIndex:     int(receipt.TransactionIndex),
			TxHash:      receipt.TxHash,
		}
	}

	return tracers.DefaultDirectory.New(*config.Tracer, tracerCtx, config.TracerConfig)
}

func isDefaultCallTracer(config *tracers.TraceConfig) bool {
	if config == nil {
		return false
	}

	if *config.Tracer != replayer.TracerName {
		return false
	}

	tracerConfig := json.RawMessage(replayer.TracerConfig)
	return slices.Equal(config.TracerConfig, tracerConfig)
}
