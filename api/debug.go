package api

import (
	"context"
	"fmt"

	gethCommon "github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	gethParams "github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/goccy/go-json"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/evm"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"

	flowEVM "github.com/onflow/flow-go/fvm/evm"
	offchain "github.com/onflow/flow-go/fvm/evm/offchain/storage"

	// this import is needed for side-effects, because the
	// tracers.DefaultDirectory is relying on the init function
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
)

// txTraceResult is the result of a single transaction trace.
type txTraceResult struct {
	TxHash gethCommon.Hash `json:"txHash"`           // transaction hash
	Result any             `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string          `json:"error,omitempty"`  // Trace failure produced by the tracer
}

type DebugAPI struct {
	registerStore  *pebble.RegisterStorage
	logger         zerolog.Logger
	tracer         storage.TraceIndexer
	blocks         storage.BlockIndexer
	transactions   storage.TransactionIndexer
	receipts       storage.ReceiptIndexer
	client         *requester.CrossSporkClient
	config         config.Config
	rateLimiter    RateLimiter
	evmChainConfig *gethParams.ChainConfig
}

func NewDebugAPI(
	registerStore *pebble.RegisterStorage,
	tracer storage.TraceIndexer,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	client *requester.CrossSporkClient,
	config config.Config,
	logger zerolog.Logger,
	rateLimiter RateLimiter,
) *DebugAPI {
	evmChainConfig := emulator.MakeChainConfig(config.EVMNetworkID)

	return &DebugAPI{
		registerStore:  registerStore,
		logger:         logger,
		tracer:         tracer,
		blocks:         blocks,
		transactions:   transactions,
		receipts:       receipts,
		client:         client,
		config:         config,
		rateLimiter:    rateLimiter,
		evmChainConfig: evmChainConfig,
	}
}

// TraceTransaction will return a debug execution trace of a transaction, if it exists.
func (d *DebugAPI) TraceTransaction(
	ctx context.Context,
	hash gethCommon.Hash,
	config *tracers.TraceConfig,
) (json.RawMessage, error) {
	if err := d.rateLimiter.Apply(ctx, DebugTraceTransaction); err != nil {
		return nil, err
	}

	return d.traceTransaction(hash, config)
}

func (d *DebugAPI) TraceBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	config *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	if err := d.rateLimiter.Apply(ctx, DebugTraceBlockByNumber); err != nil {
		return nil, err
	}

	results, err := d.traceBlockByNumber(number, config)
	if err != nil {
		return nil, err
	}

	return results, nil
}

func (d *DebugAPI) TraceBlockByHash(
	ctx context.Context,
	hash gethCommon.Hash,
	config *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	if err := d.rateLimiter.Apply(ctx, DebugTraceBlockByHash); err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByID(hash)
	if err != nil {
		return nil, err
	}

	return d.traceBlockByNumber(rpc.BlockNumber(block.Height), config)
}

func (d *DebugAPI) TraceCall(
	ctx context.Context,
	args ethTypes.TransactionArgs,
	blockNrOrHash rpc.BlockNumberOrHash,
	config *tracers.TraceCallConfig,
) (any, error) {
	if err := d.rateLimiter.Apply(ctx, DebugTraceCall); err != nil {
		return nil, err
	}

	tx := args.ToTransaction(gethTypes.LegacyTxType, BlockGasLimit)

	// Default address in case user does not provide one
	from := d.config.Coinbase
	if args.From != nil {
		from = *args.From
	}

	if config == nil {
		config = &tracers.TraceCallConfig{}
	}

	tracer, err := d.tracerForReceipt(&config.TraceConfig, nil)
	if err != nil {
		return nil, err
	}

	height, err := resolveBlockTag(&blockNrOrHash, d.blocks, d.logger)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	cdcHeight, err := d.blocks.GetCadenceHeight(height)
	if err != nil {
		return nil, err
	}

	blocksProvider := requester.NewOverridableBlocksProvider(
		d.blocks,
		d.config.FlowNetworkID,
		tracer,
	)

	if config.BlockOverrides != nil {
		blocksProvider = blocksProvider.WithBlockOverrides(&ethTypes.BlockOverrides{
			Number:       config.BlockOverrides.Number,
			Time:         config.BlockOverrides.Time,
			FeeRecipient: config.BlockOverrides.FeeRecipient,
			PrevRandao:   config.BlockOverrides.PrevRandao,
		})
	}
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
	if tx.To() != nil {
		to = *tx.To()
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
				opts = append(opts, query.WithStateOverrideBalance(addr, account.Balance.ToInt()))
			}
			if account.State != nil && account.StateDiff != nil {
				return nil, fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
			}
			// Replace entire state if caller requires.
			if account.State != nil {
				opts = append(opts, query.WithStateOverrideState(addr, account.State))
			}
			// Apply state diff into specified accounts.
			if account.StateDiff != nil {
				opts = append(opts, query.WithStateOverrideStateDiff(addr, account.StateDiff))
			}
		}
	}
	_, err = view.DryCall(
		from,
		to,
		tx.Data(),
		tx.SetCodeAuthorizations(),
		tx.Value(),
		tx.Gas(),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return tracer.GetResult()
}

// FlowHeightByBlock returns the Flow height for the given EVM block specified either by EVM
// block height or EVM block hash.
func (d *DebugAPI) FlowHeightByBlock(
	ctx context.Context,
	blockNrOrHash rpc.BlockNumberOrHash,
) (uint64, error) {
	if err := d.rateLimiter.Apply(ctx, DebugFlowHeightByBlock); err != nil {
		return 0, err
	}

	height, err := resolveBlockTag(&blockNrOrHash, d.blocks, d.logger)
	if err != nil {
		return 0, err
	}

	cdcHeight, err := d.blocks.GetCadenceHeight(height)
	if err != nil {
		return 0, err
	}

	return cdcHeight, nil
}

func (d *DebugAPI) traceTransaction(
	hash gethCommon.Hash,
	config *tracers.TraceConfig,
) (json.RawMessage, error) {
	receipt, err := d.receipts.GetByTransactionID(hash)
	if err != nil {
		return nil, err
	}

	block, err := d.blocks.GetByHeight(receipt.BlockNumber.Uint64())
	if err != nil {
		return nil, err
	}

	blockExecutor, err := d.executorAtBlock(block)
	if err != nil {
		return nil, err
	}

	tracer, err := d.tracerForReceipt(config, receipt)
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

		if err = blockExecutor.Run(tx, receipt, txTracer); err != nil {
			return nil, err
		}
	}

	if txTracer != nil {
		return txTracer.GetResult()
	}

	return nil, fmt.Errorf("failed to trace transaction with hash: %s", hash)
}

func (d *DebugAPI) traceBlockByNumber(
	number rpc.BlockNumber,
	config *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	height, err := resolveBlockNumber(number, d.blocks)
	if err != nil {
		return nil, err
	}
	block, err := d.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	results := make([]*txTraceResult, len(block.TransactionHashes))

	blockExecutor, err := d.executorAtBlock(block)
	if err != nil {
		return nil, err
	}

	receipts, err := d.receipts.GetByBlockHeight(block.Height)
	if err != nil {
		return nil, err
	}

	for i, receipt := range receipts {
		txHash := receipt.TxHash
		tx, err := d.transactions.Get(txHash)
		if err != nil {
			return nil, err
		}

		tracer, err := d.tracerForReceipt(config, receipt)
		if err != nil {
			return nil, err
		}

		if err = blockExecutor.Run(tx, receipt, tracer); err != nil {
			results[i] = &txTraceResult{TxHash: txHash, Error: err.Error()}
		} else if txTrace, err := tracer.GetResult(); err != nil {
			results[i] = &txTraceResult{TxHash: txHash, Error: err.Error()}
		} else {
			results[i] = &txTraceResult{TxHash: txHash, Result: txTrace}
		}
	}

	return results, nil
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

	// create storage
	state := offchain.NewEphemeralStorage(offchain.NewReadOnlyStorage(snapshot))

	return evm.NewBlockExecutor(
		block,
		state,
		d.config.FlowNetworkID,
		d.blocks,
		d.receipts,
		d.logger,
	), nil
}

func (d *DebugAPI) tracerForReceipt(
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

	return tracers.DefaultDirectory.New(
		*config.Tracer,
		tracerCtx,
		config.TracerConfig,
		d.evmChainConfig,
	)
}
