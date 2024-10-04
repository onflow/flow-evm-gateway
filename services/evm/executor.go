package evm

import (
	"fmt"
	"math/big"

	"github.com/holiman/uint256"
	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/precompiles"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/tracing"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

type BlockExecutor struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	chainID       flowGo.ChainID
	block         *models.Block
	blocks        storage.BlockIndexer
	logger        zerolog.Logger
	receipts      storage.ReceiptIndexer

	// block dynamic data
	txIndex uint
	gasUsed uint64
}

func NewBlockExecutor(
	block *models.Block,
	ledger atree.Ledger,
	chainID flowGo.ChainID,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) (*BlockExecutor, error) {
	logger = logger.With().Str("component", "state-execution").Logger()
	storageAddress := evm.StorageAccountAddress(chainID)

	stateDB, err := state.NewStateDB(ledger, storageAddress)
	if err != nil {
		return nil, err
	}

	return &BlockExecutor{
		emulator: emulator.NewEmulator(ledger, storageAddress),
		StateDB:  stateDB,
		chainID:  chainID,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
		logger:   logger,
	}, nil
}

func (s *BlockExecutor) Run(
	tx models.Transaction,
	tracer *tracers.Tracer,
) (*gethTypes.Receipt, error) {
	l := s.logger.With().Str("tx-hash", tx.Hash().String()).Logger()
	l.Info().Msg("executing new transaction")

	receipt, err := s.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return nil, err
	}

	ctx, err := s.blockContext(receipt)
	ctx.Tracer = tracer
	if err != nil {
		return nil, err
	}

	bv, err := s.emulator.NewBlockView(ctx)
	if err != nil {
		return nil, err
	}

	var res *types.Result

	switch t := tx.(type) {
	case models.DirectCall:
		res, err = bv.DirectCall(t.DirectCall)
	case models.TransactionCall:
		res, err = bv.RunTransaction(t.Transaction)
	default:
		return nil, fmt.Errorf("invalid transaction type")
	}

	if err != nil {
		// todo is this ok, the service would restart and retry?
		return nil, err
	}

	// we should never produce invalid transaction, since if the transaction was emitted from the evm core
	// it must have either been successful or failed, invalid transactions are not emitted
	if res.Invalid() {
		return nil, fmt.Errorf("invalid transaction %s: %w", tx.Hash(), res.ValidationError)
	}

	// increment values as part of a virtual block
	s.gasUsed += res.GasConsumed
	s.txIndex++

	l.Debug().Msg("transaction executed successfully")

	return res.LightReceipt().ToReceipt(), nil
}

func (s *BlockExecutor) Call(
	from common.Address,
	data []byte,
	tracer *tracers.Tracer,
) (*types.Result, error) {
	ctx, err := s.blockContext(nil)
	ctx.Tracer = tracer
	if err != nil {
		return nil, err
	}

	bv, err := s.emulator.NewBlockView(ctx)
	if err != nil {
		return nil, err
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(data); err != nil {
		return nil, err
	}

	return bv.DryRunTransaction(tx, from)
}

func (s *BlockExecutor) ApplyStateOverrides(config *tracers.TraceCallConfig) error {
	if config == nil || config.StateOverrides == nil {
		return nil
	}

	diff := *config.StateOverrides
	for addr, account := range diff {
		// Override account nonce.
		if account.Nonce != nil {
			s.StateDB.SetNonce(addr, uint64(*account.Nonce))
		}
		// Override account(contract) code.
		if account.Code != nil {
			s.StateDB.SetCode(addr, *account.Code)
		}
		// Override account balance.
		if account.Balance != nil {
			u256Balance, _ := uint256.FromBig((*big.Int)(*account.Balance))
			currentBalance := s.StateDB.GetBalance(addr)
			// If given balance is greater-or-equal than the current balance,
			// we need to add the diff to the current balance. Otherwise,
			// we need to sub the diff from the current balance.
			if u256Balance.Cmp(currentBalance) >= 0 {
				diff := u256Balance.Sub(u256Balance, currentBalance)
				s.StateDB.AddBalance(addr, diff, tracing.BalanceChangeUnspecified)
			} else {
				diff := currentBalance.Sub(currentBalance, u256Balance)
				s.StateDB.SubBalance(addr, diff, tracing.BalanceChangeUnspecified)
			}
		}
		if account.State != nil && account.StateDiff != nil {
			return fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
		}
		// Replace entire state if caller requires.
		if account.State != nil {
			for key, value := range *account.State {
				s.StateDB.SetState(addr, key, value)
			}
		}
		// Apply state diff into specified accounts.
		if account.StateDiff != nil {
			for key, value := range *account.StateDiff {
				s.StateDB.SetState(addr, key, value)
			}
		}
	}

	return s.StateDB.Commit(true)
}

// blockContext produces a context that is used by the block view during the execution.
// It can be used for transaction execution and calls. Receipt is not required when
// producing the context for calls.
func (s *BlockExecutor) blockContext(receipt *models.Receipt) (types.BlockContext, error) {
	ctx := types.BlockContext{
		ChainID:                types.EVMChainIDFromFlowChainID(s.chainID),
		BlockNumber:            s.block.Height,
		BlockTimestamp:         s.block.Timestamp,
		DirectCallBaseGasUsage: types.DefaultDirectCallBaseGasUsage, // todo check
		DirectCallGasPrice:     types.DefaultDirectCallGasPrice,
		GasFeeCollector:        types.CoinbaseAddress,
		GetHashFunc: func(n uint64) common.Hash {
			// For future block heights, return an empty block hash.
			if n > s.block.Height {
				return common.Hash{}
			}
			// If the given block height, is more than 256 blocks
			// in the past, return an empty block hash.
			if s.block.Height-n > 256 {
				return common.Hash{}
			}

			block, err := s.blocks.GetByHeight(n)
			if err != nil {
				panic(err)
			}
			blockHash, err := block.Hash()
			if err != nil {
				panic(err)
			}

			return blockHash
		},
		Random:            s.block.PrevRandao,
		TxCountSoFar:      s.txIndex,
		TotalGasUsedSoFar: s.gasUsed,
		Tracer:            nil,
	}

	// only add precompile cadence arch mocks if we have a receipt,
	// in case of call and dry run we don't produce receipts
	// todo when a call is made that uses cadence arch precompiles, it will fail, because
	// the precompiled contracts won't be set since we don't have a receipt for them
	// this failure should be detected and we should in such a case execute a call against the
	// EN using an AN
	if receipt != nil {
		calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
		if err != nil {
			return types.BlockContext{}, err
		}

		ctx.ExtraPrecompiledContracts = precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)
	}

	return ctx, nil
}
