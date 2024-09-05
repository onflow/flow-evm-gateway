package state

import (
	"fmt"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/precompiles"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

type State struct {
	types.StateDB // todo change to types.ReadOnlyView
	emulator      types.Emulator
	block         *models.Block
	blocks        storage.BlockIndexer
	receipts      storage.ReceiptIndexer
}

func NewState(
	block *models.Block,
	ledger atree.Ledger,
	chainID flowGo.ChainID,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
) (*State, error) {
	storageAddress := evm.StorageAccountAddress(chainID)

	// todo do we need state db?
	s, err := state.NewStateDB(ledger, storageAddress)
	if err != nil {
		return nil, err
	}

	emu := emulator.NewEmulator(ledger, storageAddress)

	return &State{
		StateDB:  s,
		emulator: emu,
		block:    block,
		blocks:   blocks,
		receipts: receipts,
	}, nil
}

func (s *State) Execute(tx models.Transaction) error {
	receipt, err := s.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return err
	}

	if receipt.Status != uint64(types.StatusSuccessful) {
		// todo should we even execute invalid transactions
		// failed we should - validate this
		fmt.Println("WRN: non successful transaction", receipt.Status, receipt.TxHash.String())
	}

	blockCtx, err := s.blockContext(receipt)
	if err != nil {
		return err
	}

	bv, err := s.emulator.NewBlockView(blockCtx)
	if err != nil {
		return err
	}

	var res *types.Result

	switch tx.(type) {
	case models.DirectCall:
		t := tx.(models.DirectCall)
		res, err = bv.DirectCall(t.DirectCall)
	case models.TransactionCall:
		t := tx.(models.TransactionCall)
		res, err = bv.RunTransaction(t.Transaction)
	default:
		return fmt.Errorf("invalid transaction type")
	}

	if err != nil {
		// todo is this ok, the service would restart and retry?
		return err
	}

	if !models.EqualReceipts(res.Receipt(), receipt) {
		// todo add maybe a log of result and expected result
		return fmt.Errorf("rexecution of transaction %s did not produce same result", tx.Hash().String())
	}

	return nil
}

func (s *State) blockContext(receipt *models.Receipt) (types.BlockContext, error) {
	calls, err := types.AggregatedPrecompileCallsFromEncoded(receipt.PrecompiledCalls)
	if err != nil {
		return types.BlockContext{}, err
	}

	precompileContracts := precompiles.AggregatedPrecompiledCallsToPrecompiledContracts(calls)

	return types.BlockContext{
		ChainID:                types.FlowEVMPreviewNetChainID, // todo configure dynamically
		BlockNumber:            s.block.Height,
		BlockTimestamp:         s.block.Timestamp,
		DirectCallBaseGasUsage: types.DefaultDirectCallBaseGasUsage, // todo check
		DirectCallGasPrice:     types.DefaultDirectCallGasPrice,
		GasFeeCollector:        types.CoinbaseAddress,
		GetHashFunc: func(n uint64) common.Hash {
			b, err := s.blocks.GetByHeight(n)
			if err != nil {
				panic(err)
			}
			h, err := b.Hash()
			if err != nil {
				panic(err)
			}

			return h
		},
		Random:                    s.block.PrevRandao,
		ExtraPrecompiledContracts: precompileContracts,
		// todo check values bellow if they are needed by the execution
		TxCountSoFar:      0,
		TotalGasUsedSoFar: 0,
		Tracer:            nil,
	}, nil
}
