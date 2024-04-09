package ingestion

import (
	"context"
	"errors"
	"fmt"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go/fvm/evm/types"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
)

var _ models.Engine = &Engine{}

type Engine struct {
	subscriber    EventSubscriber
	blocks        storage.BlockIndexer
	receipts      storage.ReceiptIndexer
	transactions  storage.TransactionIndexer
	accounts      storage.AccountIndexer
	log           zerolog.Logger
	evmLastHeight *models.SequentialHeight
	status        *models.EngineStatus
}

func NewEventIngestionEngine(
	subscriber EventSubscriber,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	transactions storage.TransactionIndexer,
	accounts storage.AccountIndexer,
	log zerolog.Logger,
) *Engine {
	log = log.With().Str("component", "ingestion").Logger()

	return &Engine{
		subscriber:   subscriber,
		blocks:       blocks,
		receipts:     receipts,
		transactions: transactions,
		accounts:     accounts,
		log:          log,
		status:       models.NewEngineStatus(),
	}
}

// Ready signals when the engine has started.
func (e *Engine) Ready() <-chan struct{} {
	return e.status.IsReady()
}

// Done signals when the engine has stopped.
func (e *Engine) Done() <-chan struct{} {
	// return e.status.IsDone()
	return nil
}

// Stop the engine.
func (e *Engine) Stop() {
	// todo
}

// Run the event ingestion engine. Load the latest height that was stored and provide it
// to the event subscribers as a starting point.
// Consume the events provided by the event subscriber.
// Each event is then processed by the event processing methods.
func (e *Engine) Run(ctx context.Context) error {
	latestCadence, err := e.blocks.LatestCadenceHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest cadence height: %w", err)
	}

	e.log.Info().Uint64("start-cadence-height", latestCadence).Msg("starting ingestion")

	events, errs, err := e.subscriber.Subscribe(ctx, latestCadence)
	if err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	e.status.MarkReady()

	for {
		select {
		case <-ctx.Done():
			e.log.Info().Msg("event ingestion received done signal")
			return nil

		case blockEvents, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return models.ErrDisconnected
			}

			err = e.processEvents(models.NewCadenceEvents(blockEvents))
			if err != nil {
				e.log.Error().Err(err).Msg("failed to process EVM events")
				return err
			}

		case err, ok := <-errs:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				return models.ErrDisconnected
			}

			return errors.Join(err, models.ErrDisconnected)
		}
	}
}

// processEvents converts the events to block and transactions and indexes them.
func (e *Engine) processEvents(events *models.CadenceEvents) error {
	e.log.Debug().
		Uint64("cadence-height", events.CadenceHeight()).
		Int("cadence-event-length", events.Length()).
		Msg("received new cadence evm events")

	// this is an optimization, because if there is a block event in batch, it will internally update latest height
	// otherwise we still want to update it explicitly, so we don't have to reindex in case of a restart
	if events.Empty() {
		if err := e.blocks.SetLatestCadenceHeight(events.CadenceHeight()); err != nil {
			return fmt.Errorf("failed to update to latest cadence height during events ingestion: %w", err)
		}
	}

	// first we index the evm block
	block, err := events.Block()
	if err != nil {
		return err
	}
	if err := e.indexBlock(events.CadenceHeight(), block); err != nil {
		return err
	}

	txs, receipts, err := events.Transactions()
	if err != nil {
		return err
	}
	for i, tx := range txs {
		if err := e.indexTransaction(tx, receipts[i]); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) indexBlock(cadenceHeight uint64, block *types.Block) error {
	if block == nil { // safety check shouldn't happen
		return fmt.Errorf("can't process empty block")
	}
	// only init latest height if not set
	if e.evmLastHeight == nil {
		e.evmLastHeight = models.NewSequentialHeight(block.Height)
	}

	// make sure the latest height is increasing sequentially or is same as latest
	if err := e.evmLastHeight.Increment(block.Height); err != nil {
		return fmt.Errorf("invalid block height, expected %d, got %d: %w", e.evmLastHeight.Load(), block.Height, err)
	}

	h, _ := block.Hash()
	e.log.Info().
		Str("hash", h.Hex()).
		Uint64("evm-height", block.Height).
		Str("parent-hash", block.ParentBlockHash.String()).
		Str("tx-hash", block.TransactionHashes[0].Hex()). // now we only have 1 tx per block
		Msg("new evm block executed event")

	return e.blocks.Store(cadenceHeight, block)
}

func (e *Engine) indexTransaction(tx models.Transaction, receipt *gethTypes.Receipt) error {
	if tx == nil || receipt == nil { // safety check shouldn't happen
		return fmt.Errorf("can't process empty tx or receipt")
	}
	// TODO(m-Peter): Remove the error return value once flow-go is updated
	txHash, err := tx.Hash()
	if err != nil {
		return fmt.Errorf("failed to compute TX hash: %w", err)
	}

	e.log.Info().
		Str("contract-address", receipt.ContractAddress.String()).
		Int("log-count", len(receipt.Logs)).
		Uint64("evm-height", receipt.BlockNumber.Uint64()).
		Str("receipt-tx-hash", receipt.TxHash.String()).
		Str("tx-hash", txHash.String()).
		Msg("ingesting new transaction executed event")

	// todo think if we could introduce batching
	if err := e.transactions.Store(tx); err != nil {
		return fmt.Errorf("failed to store tx: %w", err)
	}

	if err := e.accounts.Update(tx, receipt); err != nil {
		return fmt.Errorf("failed to update accounts: %w", err)
	}

	if err := e.receipts.Store(receipt); err != nil {
		return fmt.Errorf("failed to store receipt: %w", err)
	}

	return nil
}
