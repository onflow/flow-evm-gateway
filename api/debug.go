package api

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/goccy/go-json"
	"github.com/holiman/uint256"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/tracing"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/onflow/go-ethereum/eth/tracers/logger"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"

	evm "github.com/onflow/flow-evm-gateway/services/evm"
	// Force-load native and js packages, to trigger registration
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
	client    *evm.CrossSporkClient
	tracer    storage.TraceIndexer
	blocks    storage.BlockIndexer
	receipts  storage.ReceiptIndexer
	config    *config.Config
	logger    zerolog.Logger
	collector metrics.Collector
}

func NewDebugAPI(
	client *evm.CrossSporkClient,
	tracer storage.TraceIndexer,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	config *config.Config,
	logger zerolog.Logger,
	collector metrics.Collector,
) *DebugAPI {
	return &DebugAPI{
		client:    client,
		tracer:    tracer,
		blocks:    blocks,
		receipts:  receipts,
		config:    config,
		logger:    logger,
		collector: collector,
	}
}

// TraceTransaction will return a debug execution trace of a transaction if it exists,
// currently we only support CALL traces, so the config is ignored.
func (d *DebugAPI) TraceTransaction(
	_ context.Context,
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
	cfg *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByHeight(uint64(number.Int64()))
	if err != nil {
		return handleError[[]*txTraceResult](err, d.logger, d.collector)
	}

	return d.traceBlock(ctx, block, cfg)
}

func (d *DebugAPI) TraceBlockByHash(
	ctx context.Context,
	hash gethCommon.Hash,
	cfg *tracers.TraceConfig,
) ([]*txTraceResult, error) {
	block, err := d.blocks.GetByID(hash)
	if err != nil {
		return handleError[[]*txTraceResult](err, d.logger, d.collector)
	}

	return d.traceBlock(ctx, block, cfg)
}

func (d *DebugAPI) traceBlock(
	ctx context.Context,
	block *models.Block,
	_ *tracers.TraceConfig,
) ([]*txTraceResult, error) {
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
	txEncoded, err := encodeTxFromArgs(args)
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

	blockExecutor, err := d.executorAtBlock(block)
	if err != nil {
		return nil, err
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(txEncoded); err != nil {
		return nil, err
	}

	// call tracer
	head := &gethTypes.Header{
		Number: big.NewInt(int64(block.Height)),
		Time:   uint64(time.Now().Unix()),
	}
	emulatorConfig := emulator.NewConfig(
		emulator.WithChainID(d.config.EVMNetworkID),
		emulator.WithBlockNumber(head.Number),
		emulator.WithBlockTime(head.Time),
	)
	random := block.PrevRandao
	stateDB := blockExecutor.StateDB
	tracer.OnTxStart(
		&tracing.VMContext{
			Coinbase:    d.config.Coinbase,
			BlockNumber: head.Number,
			Time:        head.Time,
			Random:      &random,
			GasPrice:    d.config.GasPrice,
			ChainConfig: emulatorConfig.ChainConfig,
			StateDB:     stateDB,
		},
		tx,
		from,
	)

	err = applyStateOverrides(config, stateDB)
	if err != nil {
		return nil, err
	}

	result, err := blockExecutor.Call(from, txEncoded, tracer)
	if err != nil {
		return nil, err
	}

	// call tracer on tx end
	if tracer.OnTxEnd != nil {
		tracer.OnTxEnd(result.Receipt(), result.ValidationError)
	}

	return tracer.GetResult()
}

func (d *DebugAPI) executorAtBlock(block *models.Block) (*evm.BlockExecutor, error) {
	blockHeight := block.Height
	client, err := d.client.GetClientForHeight(blockHeight)
	if err != nil {
		return nil, err
	}

	exeClient, ok := client.(*grpc.Client)
	if !ok {
		return nil, fmt.Errorf("could not convert to execution client")
	}

	ledger, err := evm.NewRemoteLedger(exeClient.ExecutionDataRPCClient(), blockHeight)
	if err != nil {
		return nil, fmt.Errorf("could not create remote ledger for height: %d, with: %w", blockHeight, err)
	}

	return evm.NewBlockExecutor(
		block,
		ledger,
		d.config.FlowNetworkID,
		d.blocks,
		d.receipts,
		d.logger,
	)
}

func applyStateOverrides(config *tracers.TraceCallConfig, stateDB types.StateDB) error {
	diff := config.StateOverrides

	if diff == nil {
		return nil
	}

	for addr, account := range *diff {
		// Override account nonce.
		if account.Nonce != nil {
			stateDB.SetNonce(addr, uint64(*account.Nonce))
		}
		// Override account(contract) code.
		if account.Code != nil {
			stateDB.SetCode(addr, *account.Code)
		}
		// Override account balance.
		if account.Balance != nil {
			u256Balance, _ := uint256.FromBig((*big.Int)(*account.Balance))
			currentBalance := stateDB.GetBalance(addr)
			// If given balance is greater-or-equal than the current balance,
			// we need to add the diff to the current balance. Otherwise,
			// we need to sub the diff from the current balance.
			if u256Balance.Cmp(currentBalance) >= 0 {
				diff := u256Balance.Sub(u256Balance, currentBalance)
				stateDB.AddBalance(addr, diff, tracing.BalanceChangeUnspecified)
			} else {
				diff := currentBalance.Sub(currentBalance, u256Balance)
				stateDB.SubBalance(addr, diff, tracing.BalanceChangeUnspecified)
			}
		}
		if account.State != nil && account.StateDiff != nil {
			return fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
		}
		// Replace entire state if caller requires.
		if account.State != nil {
			for key, value := range *account.State {
				stateDB.SetState(addr, key, value)
			}
		}
		// Apply state diff into specified accounts.
		if account.StateDiff != nil {
			for key, value := range *account.StateDiff {
				stateDB.SetState(addr, key, value)
			}
		}
	}

	return stateDB.Commit(true)
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
