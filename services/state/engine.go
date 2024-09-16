package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/onflow/atree"
	"github.com/onflow/flow/protobuf/go/flow/executiondata"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/trie"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

var _ models.Engine = &Engine{}
var _ models.Subscriber = &Engine{}

type Engine struct {
	config         *config.Config
	execution      executiondata.ExecutionDataAPIClient
	logger         zerolog.Logger
	status         *models.EngineStatus
	blockPublisher *models.Publisher
	store          *pebble.Storage
	blocks         storage.BlockIndexer
	transactions   storage.TransactionIndexer
	receipts       storage.ReceiptIndexer
}

func NewStateEngine(
	config *config.Config,
	execution executiondata.ExecutionDataAPIClient,
	blockPublisher *models.Publisher,
	store *pebble.Storage,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
) *Engine {
	log := logger.With().Str("component", "state").Logger()

	return &Engine{
		config:         config,
		execution:      execution,
		logger:         log,
		store:          store,
		status:         models.NewEngineStatus(),
		blockPublisher: blockPublisher,
		blocks:         blocks,
		transactions:   transactions,
		receipts:       receipts,
	}
}

// todo rethink whether it would be more robust to rely on blocks in the storage
// instead of receiving events, relying on storage and keeping a separate count of
// transactions executed would allow for independent restart and reexecution
// if we panic with events the missed tx won't get reexecuted since it's relying on
// event ingestion also not indexing that transaction

func (e *Engine) Notify(data any) {
	block, ok := data.(*models.Block)
	if !ok {
		e.logger.Error().Msg("invalid event type sent to state ingestion")
		return
	}

	l := e.logger.With().Uint64("evm-height", block.Height).Logger()
	l.Info().Msg("received new block")

	if err := e.executeBlock(block); err != nil {
		panic(fmt.Errorf("failed to execute block at height %d: %w", block.Height, err))
	}

	l.Info().Msg("successfully executed block")
}

func (e *Engine) Run(ctx context.Context) error {
	e.blockPublisher.Subscribe(e)
	e.status.MarkReady()
	return nil
}

func (e *Engine) Stop() {
	// todo cleanup
	e.status.MarkStopped()
}

func (e *Engine) Done() <-chan struct{} {
	return e.status.IsDone()
}

func (e *Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}

func (e *Engine) Error() <-chan error {
	return nil
}

func (e *Engine) ID() uuid.UUID {
	return uuid.New()
}

// executeBlock will execute all transactions in the provided block.
// If a transaction fails to execute or the result doesn't match expected
// result return an error.
// Transaction executed should match a receipt we have indexed from the network
// produced by execution nodes. This check makes sure we keep a correct state.
func (e *Engine) executeBlock(block *models.Block) error {
	var registers atree.Ledger
	registers = pebble.NewRegister(e.store, block.Height)

	// if validation is enabled wrap the register ledger into a validator
	if e.config.ValidateRegisters {
		registers = NewRegisterValidator(registers, nil)
	}

	state, err := NewBlockState(block, registers, e.config.FlowNetworkID, e.blocks, e.receipts, e.logger)
	if err != nil {
		return err
	}

	receipts := make(gethTypes.Receipts, len(block.TransactionHashes))

	for i, h := range block.TransactionHashes {
		tx, err := e.transactions.Get(h)
		if err != nil {
			return err
		}

		receipt, err := state.Execute(tx)
		if err != nil {
			return fmt.Errorf("failed to execute tx %s: %w", h, err)
		}
		receipts[i] = receipt
	}

	executedRoot := gethTypes.DeriveSha(receipts, trie.NewStackTrie(nil))
	// make sure receipt root matches, so we know all the execution results are same
	if executedRoot.Cmp(block.ReceiptRoot) != 0 {
		return errs.ErrStateMismatch
	}

	if e.config.ValidateRegisters {
		validator := registers.(*RegisterValidator)
		if err := validator.ValidateBlock(block.Height); err != nil {
			if errors.Is(err, errs.ErrStateMismatch) {
				return err
			}
			// if there were issues with the client request only log the error
			e.logger.Error().Err(err).Msg("register validation failed")
		}
	}

	// update executed block height
	return e.blocks.SetExecutedHeight(block.Height)
}
