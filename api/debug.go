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
	client       *evm.CrossSporkClient
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	config       *config.Config
	logger       zerolog.Logger
	collector    metrics.Collector
}

func NewDebugAPI(
	client *evm.CrossSporkClient,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	config *config.Config,
	logger zerolog.Logger,
	collector metrics.Collector,
) *DebugAPI {
	return &DebugAPI{
		client:       client,
		blocks:       blocks,
		transactions: transactions,
		receipts:     receipts,
		config:       config,
		logger:       logger,
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

	results := make([]*txTraceResult, len(block.TransactionHashes))
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
	chainConfig := emulatorConfig.ChainConfig
	random := block.PrevRandao
	stateDB := blockExecutor.StateDB

	diff := config.StateOverrides
	if diff != nil {
		for addr, account := range *diff {
			// Replace entire state if caller requires.
			if account.State != nil {
				stateDB.Selfdestruct6780(addr)
				for key, value := range *account.State {
					stateDB.SetState(addr, key, value)
				}
			}
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
				stateDB.AddBalance(addr, u256Balance, tracing.BalanceChangeUnspecified)
			}
			if account.State != nil && account.StateDiff != nil {
				return nil, fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
			}
			// Apply state diff into specified accounts.
			if account.StateDiff != nil {
				for key, value := range *account.StateDiff {
					fmt.Println("Key: ", key)
					fmt.Println("Value: ", value)
					stateDB.SetState(addr, key, value)
				}
			}
		}
		// Now finalize the changes. Finalize is normally performed between transactions.
		// By using finalize, the overrides are semantically behaving as
		// if they were created in a transaction just before the tracing occur.
		stateDB.Commit(true)
	}

	tracer.OnTxStart(
		&tracing.VMContext{
			Coinbase:    d.config.Coinbase,
			BlockNumber: head.Number,
			Time:        head.Time,
			Random:      &random,
			GasPrice:    d.config.GasPrice,
			ChainConfig: chainConfig,
			StateDB:     stateDB,
		},
		tx,
		from,
	)

	stateVal := stateDB.GetState(
		gethCommon.HexToAddress(args.To.Hex()),
		gethCommon.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
	)
	fmt.Println("[StateValue]: ", stateVal)
	//stateDB.SetState(addr, key, value)

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
