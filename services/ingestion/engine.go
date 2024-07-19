package ingestion

import (
	"context"
	"fmt"

	pebbleDB "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

var _ models.Engine = &Engine{}

type Engine struct {
	subscriber            EventSubscriber
	store                 *pebble.Storage
	blocks                storage.BlockIndexer
	receipts              storage.ReceiptIndexer
	transactions          storage.TransactionIndexer
	accounts              storage.AccountIndexer
	log                   zerolog.Logger
	evmLastHeight         *models.SequentialHeight
	status                *models.EngineStatus
	blocksPublisher       *models.Publisher
	transactionsPublisher *models.Publisher
	logsPublisher         *models.Publisher
}

func NewEventIngestionEngine(
	subscriber EventSubscriber,
	store *pebble.Storage,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	transactions storage.TransactionIndexer,
	accounts storage.AccountIndexer,
	blocksPublisher *models.Publisher,
	transactionsPublisher *models.Publisher,
	logsPublisher *models.Publisher,
	log zerolog.Logger,
) *Engine {
	log = log.With().Str("component", "ingestion").Logger()

	return &Engine{
		subscriber:            subscriber,
		store:                 store,
		blocks:                blocks,
		receipts:              receipts,
		transactions:          transactions,
		accounts:              accounts,
		log:                   log,
		status:                models.NewEngineStatus(),
		blocksPublisher:       blocksPublisher,
		transactionsPublisher: transactionsPublisher,
		logsPublisher:         logsPublisher,
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

// Run the Cadence event ingestion engine.
//
// Cadence event ingestion engine subscribes to all new EVM related events on Flow network,
// currently there are two types of events:
// - evm.BlockExecuted: this event is emitted when a new EVM block is created (usually with any new transactions)
// - evm.TransactionExecuted: this event is emitted when a new EVM transaction is executed (even if failed)
// Each event that is received should contain at least block event, but usually also transaction events.
// There can be multiple transaction events for a single Cadence height, but only a single block event.
// Events are after indexed in-order, first block event and then all transaction events.
//
// Expected errors:
// there is a disconnected error which is a recoverable error that can be expected and should be
// handled by restarting the engine. This can happen if the client connection to the event subscription
// drops.
// All other errors are unexpected.
func (e *Engine) Run(ctx context.Context) error {
	latestCadence, err := e.blocks.LatestCadenceHeight()
	if err != nil {
		return fmt.Errorf("failed to get latest cadence height: %w", err)
	}

	e.log.Info().Uint64("start-cadence-height", latestCadence).Msg("starting ingestion")

	e.status.MarkReady()

	for events := range e.subscriber.Subscribe(ctx, latestCadence) {
		if events.Err != nil {
			return fmt.Errorf("failure in event subscription: %w", events.Err)
		}

		err = e.processEvents(events.Events)
		if err != nil {
			e.log.Error().Err(err).Msg("failed to process EVM events")
			return err
		}
	}

	return nil
}

// processEvents converts the events to block and transactions and indexes them.
//
// BlockEvents are received by the access node API and contain Cadence height (always a single Flow block),
// and a slice of events. In our case events are EVM events that can contain 0 or multiple EVM blocks and
// 0 or multiple EVM transactions. The case where we have 0 blocks and transactions is a special heartbeat
// event that is emitted if there are no new EVM events for a longer period of time
// (configurable on AN normally a couple of seconds).
//
// The values for events payloads are defined in flow-go:
// https://github.com/onflow/flow-go/blob/master/fvm/evm/types/events.go
//
// Any error is unexpected and fatal.
func (e *Engine) processEvents(events *models.CadenceEvents) error {
	e.log.Info().
		Uint64("cadence-height", events.CadenceHeight()).
		Int("cadence-event-length", events.Length()).
		Msg("received new cadence evm events")

	// if heartbeat interval with no data still update the cadence height
	if events.Empty() {
		if err := e.blocks.SetLatestCadenceHeight(events.CadenceHeight(), nil); err != nil {
			return fmt.Errorf("failed to update to latest cadence height during events ingestion: %w", err)
		}
		return nil // nothing else to do this was heartbeat event with not event payloads
	}

	batch := e.store.NewBatch()
	defer batch.Close()

	// we first index evm blocks only then transactions if any present
	blocks, err := events.Blocks()
	if err != nil {
		return err
	}
	for _, block := range blocks {
		err := e.indexBlock(
			events.CadenceHeight(),
			events.CadenceBlockID(),
			block,
			batch,
		)
		if err != nil {
			return fmt.Errorf("failed to index block %d event: %w", block.Height, err)
		}
	}

	txs, receipts, err := events.Transactions()
	if err != nil {
		return err
	}
	for i, tx := range txs {
		if err := e.indexTransaction(tx, receipts[i], batch); err != nil {
			return fmt.Errorf("failed to index transaction %s event: %w", tx.Hash().String(), err)
		}
	}

	if err := batch.Commit(pebbleDB.Sync); err != nil {
		return fmt.Errorf("failed to commit indexed data for Cadence block %d: %w", events.CadenceHeight(), err)
	}

	// emit events for each block, transaction and logs, only after we successfully commit the data
	for _, b := range blocks {
		e.blocksPublisher.Publish(b)
	}

	for i, r := range receipts {
		e.transactionsPublisher.Publish(txs[i])

		if len(r.Logs) > 0 {
			e.logsPublisher.Publish(r.Logs)
		}
	}

	return nil
}

func (e *Engine) indexBlock(
	cadenceHeight uint64,
	cadenceID flow.Identifier,
	block *types.Block,
	batch *pebbleDB.Batch,
) error {
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

	blockHash, _ := block.Hash()
	txHashes := make([]string, len(block.TransactionHashes))
	for i, t := range block.TransactionHashes {
		txHashes[i] = t.Hex()
	}
	e.log.Info().
		Str("hash", blockHash.Hex()).
		Uint64("evm-height", block.Height).
		Uint64("cadence-height", cadenceHeight).
		Str("cadence-id", cadenceID.String()).
		Str("parent-hash", block.ParentBlockHash.String()).
		Strs("tx-hashes", txHashes).
		Msg("new evm block executed event")

	return e.blocks.Store(cadenceHeight, cadenceID, block, batch)
}

func (e *Engine) indexTransaction(
	tx models.Transaction,
	receipt *models.StorageReceipt,
	batch *pebbleDB.Batch,
) error {
	if tx == nil || receipt == nil { // safety check shouldn't happen
		return fmt.Errorf("can't process empty tx or receipt")
	}

	e.log.Info().
		Str("contract-address", receipt.ContractAddress.String()).
		Int("log-count", len(receipt.Logs)).
		Uint64("evm-height", receipt.BlockNumber.Uint64()).
		Uint("tx-index", receipt.TransactionIndex).
		Str("tx-hash", tx.Hash().String()).
		Msg("ingesting new transaction executed event")

	if err := e.transactions.Store(tx, batch); err != nil {
		return fmt.Errorf("failed to store tx: %w", err)
	}

	if err := e.accounts.Update(tx, receipt, batch); err != nil {
		return fmt.Errorf("failed to update accounts: %w", err)
	}

	if err := e.receipts.Store(receipt, batch); err != nil {
		return fmt.Errorf("failed to store receipt: %w", err)
	}

	return nil
}
