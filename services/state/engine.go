package state

import (
	"context"
	"errors"
	"fmt"

	pebbleDB "github.com/cockroachdb/pebble"
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

// Engine state engine takes care of creating a local state by
// re-executing each block against the local emulator and local
// register index.
// The engine relies on the block publisher to receive new
// block events which is done by the event ingestion engine.
// It also relies on the event ingestion engine to wait for the
// state engine to be ready before subscribing, because on startup
// we have to do a sync between last indexed and last executed block
// during which time we should not receive any other block events.
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

// Notify will get new events for blocks from the blocks publisher,
// which is being produced by the event ingestion engine.
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
	// check if we need to execute any blocks that were indexed but not executed
	// this could happen if after index but before execution the node crashes
	indexed, err := e.blocks.LatestIndexedHeight()
	if err != nil {
		return err
	}

	executed, err := e.blocks.LatestExecutedHeight()
	if err != nil {
		return err
	}

	if executed < indexed {
		e.logger.Info().
			Uint64("last-executed", executed).
			Uint64("last-indexed", indexed).
			Msg("syncing executed blocks on startup")

		for i := executed; i <= indexed; i++ {
			block, err := e.blocks.GetByHeight(i)
			if err != nil {
				return err
			}

			if err := e.executeBlock(block); err != nil {
				return err
			}
		}
	}

	// after all is up to sync we subscribe to live blocks
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
	// start a new database batch
	batch := e.store.NewBatch()
	defer func() {
		if err := batch.Close(); err != nil {
			e.logger.Warn().Err(err).Msg("failed to close batch")
		}
	}()

	var registers atree.Ledger
	registers = pebble.NewRegister(e.store, block.Height, batch)

	// if validation is enabled wrap the register ledger into a validator
	if e.config.ValidateRegisters {
		registers = NewRegisterValidator(registers, e.execution)
	}

	state, err := NewBlockState(
		block,
		registers,
		e.config.FlowNetworkID,
		e.blocks,
		e.receipts,
		e.logger,
		nil,
	)
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
		if err := e.validateBlock(registers, block); err != nil {
			return err
		}
	}

	if err := e.blocks.SetExecutedHeight(block.Height); err != nil {
		return err
	}

	if err := batch.Commit(pebbleDB.Sync); err != nil {
		return fmt.Errorf("failed to commit executed data for block %d: %w", block.Height, err)
	}

	return nil
}

// validateBlock validates the block updated registers using the register validator.
// If there's any register mismatch it returns an error.
//
// todo remove:
// Currently, this is done synchronous but could be improved in the future, however this register
// validation using the AN APIs will be completely replaced with the state commitment checksum once
// the work is done on core: https://github.com/onflow/flow-go/pull/6451
func (e *Engine) validateBlock(registers atree.Ledger, block *models.Block) error {
	validator, ok := registers.(*RegisterValidator)
	if !ok {
		return fmt.Errorf("invalid register validator used")
	}

	cadenceHeight, err := e.blocks.GetCadenceHeight(block.Height)
	if err != nil {
		e.logger.Error().Err(err).Msg("register validation failed, block cadence height")
	}

	if err := validator.ValidateBlock(cadenceHeight); err != nil {
		if errors.Is(err, errs.ErrStateMismatch) {
			return err
		}
		// if there were issues with the client request only log the error
		e.logger.Error().Err(err).Msg("register validation failed")
	}

	return nil
}
