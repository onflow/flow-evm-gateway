package ingestion

import (
	"context"
	"fmt"
	"time"

	flowGo "github.com/onflow/flow-go/model/flow"

	pebbleDB "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go-sdk"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"

	"github.com/onflow/flow-go/fvm/evm/offchain/sync"
)

var _ models.Engine = &Engine{}

// Engine is an implementation of the event ingestion engine.
//
// This engine monitors the Flow network for two types of EVM events:
//   - block executed: emitted anytime a new block is created on the network,
//     it is representation of an EVM block and contains all the consensus information.
//   - transaction executed: emitted anytime a new transaction is executed
//     independently of block event. This is similar to EVM transaction receipt and
//     contains information about the transaction execution, like result, gas used etc.
//
// The ingested events explained above are then indexed in a local database and
// used in any queries from the RPC APIs.
// Ingestion of the events is idempotent so if a reindex needs to happen it can, since
// it will just overwrite the current indexed data. Idempotency is an important
// requirement of the implementation of this engine.
type Engine struct {
	*models.EngineStatus

	subscriber      EventSubscriber
	blocksProvider  *replayer.BlocksProvider
	store           *pebble.Storage
	registerStore   *pebble.RegisterStorage
	blocks          storage.BlockIndexer
	receipts        storage.ReceiptIndexer
	transactions    storage.TransactionIndexer
	traces          storage.TraceIndexer
	log             zerolog.Logger
	evmLastHeight   *models.SequentialHeight
	blocksPublisher *models.Publisher[*models.Block]
	logsPublisher   *models.Publisher[[]*gethTypes.Log]
	collector       metrics.Collector
	replayerConfig  replayer.Config
}

func NewEventIngestionEngine(
	subscriber EventSubscriber,
	blocksProvider *replayer.BlocksProvider,
	store *pebble.Storage,
	registerStore *pebble.RegisterStorage,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	transactions storage.TransactionIndexer,
	traces storage.TraceIndexer,
	blocksPublisher *models.Publisher[*models.Block],
	logsPublisher *models.Publisher[[]*gethTypes.Log],
	log zerolog.Logger,
	collector metrics.Collector,
	replayerConfig replayer.Config,
) *Engine {
	log = log.With().Str("component", "ingestion").Logger()

	return &Engine{
		EngineStatus: models.NewEngineStatus(),

		subscriber:      subscriber,
		blocksProvider:  blocksProvider,
		store:           store,
		registerStore:   registerStore,
		blocks:          blocks,
		receipts:        receipts,
		transactions:    transactions,
		traces:          traces,
		log:             log,
		blocksPublisher: blocksPublisher,
		logsPublisher:   logsPublisher,
		collector:       collector,
		replayerConfig:  replayerConfig,
	}
}

// Stop the engine.
func (e *Engine) Stop() {
	e.MarkDone()
	<-e.Stopped()
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
	e.log.Info().Msg("starting ingestion")

	e.MarkReady()
	defer e.MarkStopped()

	events := e.subscriber.Subscribe(ctx)

	for {
		select {
		case <-e.Done():
			// stop the engine
			return nil
		case events, ok := <-events:
			if !ok {
				return nil
			}
			if events.Err != nil {
				return fmt.Errorf(
					"failure in event subscription with: %w",
					events.Err,
				)
			}

			err := e.processEvents(events.Events)
			if err != nil {
				e.log.Error().Err(err).Msg("failed to process EVM events")
				return err
			}
		}
	}
}

// withBatch will execute the provided function with a new batch, and commit the batch
// afterwards if no error is returned.
func (e *Engine) withBatch(f func(batch *pebbleDB.Batch) error) error {
	batch := e.store.NewBatch()
	defer func(batch *pebbleDB.Batch) {
		err := batch.Close()
		if err != nil {
			e.log.Fatal().Err(err).Msg("failed to close batch")
		}
	}(batch)

	err := f(batch)
	if err != nil {
		return err
	}

	if err := batch.Commit(pebbleDB.Sync); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
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

	err := e.withBatch(
		func(batch *pebbleDB.Batch) error {
			return e.indexEvents(events, batch)
		},
	)
	if err != nil {
		return fmt.Errorf("failed to index events for cadence block %d: %w", events.CadenceHeight(), err)
	}

	e.collector.CadenceHeightIndexed(events.CadenceHeight())

	if events.Empty() {
		return nil // nothing else to do this was heartbeat event with not event payloads
	}

	// emit block event and logs, only after we successfully commit the data
	e.blocksPublisher.Publish(events.Block())
	for _, r := range events.Receipts() {
		if len(r.Logs) > 0 {
			e.logsPublisher.Publish(r.Logs)
		}
	}

	e.collector.EVMTransactionIndexed(len(events.Transactions()))
	e.collector.EVMHeightIndexed(events.Block().Height)
	return nil
}

// indexEvents will replay the evm transactions using the block events and index all results.
func (e *Engine) indexEvents(events *models.CadenceEvents, batch *pebbleDB.Batch) error {
	// if heartbeat interval with no data still update the cadence height
	if events.Empty() {
		if err := e.blocks.SetLatestCadenceHeight(events.CadenceHeight(), batch); err != nil {
			return fmt.Errorf(
				"failed to update to latest cadence height: %d, during events ingestion: %w",
				events.CadenceHeight(),
				err,
			)
		}
		return nil // nothing else to do this was heartbeat event with not event payloads
	}

	// Step 1: Re-execute all transactions on the latest EVM block

	// Step 1.1: Notify the `BlocksProvider` of the newly received EVM block
	if err := e.blocksProvider.OnBlockReceived(events.Block()); err != nil {
		return err
	}

	replayer := sync.NewReplayer(
		e.replayerConfig.ChainID,
		e.replayerConfig.RootAddr,
		e.registerStore,
		e.blocksProvider,
		e.log,
		e.replayerConfig.CallTracerCollector.TxTracer(),
		e.replayerConfig.ValidateResults,
	)

	// Step 1.2: Replay all block transactions
	// If `ReplayBlock` returns any error, we abort the EVM events processing
	blockEvents := events.BlockEventPayload()
	res, err := replayer.ReplayBlock(events.TxEventPayloads(), blockEvents)
	if err != nil {
		return fmt.Errorf("failed to replay block on height: %d, with: %w", events.Block().Height, err)
	}

	// Step 2: Write all the necessary changes to each storage

	// Step 2.1: Write all the EVM state changes to `StorageProvider`
	err = e.registerStore.Store(registerEntriesFromKeyValue(res.StorageRegisterUpdates()), blockEvents.Height, batch)
	if err != nil {
		return fmt.Errorf("failed to store state changes on block: %d", events.Block().Height)
	}

	// Step 2.2: Write the latest EVM block to `Blocks` storage
	// This verifies the EVM height is sequential, and if not it will return an error
	// TODO(janezp): can we do this before re-execution of the block?
	err = e.indexBlock(
		events.CadenceHeight(),
		events.CadenceBlockID(),
		events.Block(),
		batch,
	)
	if err != nil {
		return fmt.Errorf("failed to index block %d event: %w", events.Block().Height, err)
	}

	// Step 2.3: Write all EVM transactions of the current block,
	// to `Transactions` storage
	for i, tx := range events.Transactions() {
		receipt := events.Receipts()[i]

		err := e.indexTransaction(tx, receipt, batch)
		if err != nil {
			return fmt.Errorf("failed to index transaction %s event: %w", tx.Hash().String(), err)
		}
	}

	// Step 2.4: Write all EVM transaction receipts of the current block,
	// to `Receipts` storage
	err = e.indexReceipts(events.Receipts(), batch)
	if err != nil {
		return fmt.Errorf("failed to index receipts for block %d event: %w", events.Block().Height, err)
	}

	traceCollector := e.replayerConfig.CallTracerCollector
	for _, tx := range events.Transactions() {
		txHash := tx.Hash()
		traceResult, err := traceCollector.Collect(txHash)
		if err != nil {
			return err
		}

		err = e.traces.StoreTransaction(txHash, traceResult, batch)
		if err != nil {
			return err
		}
	}

	blockCreation := time.Unix(int64(events.Block().Timestamp), 0)
	e.collector.BlockIngestionTime(blockCreation)

	return nil
}

func (e *Engine) indexBlock(
	cadenceHeight uint64,
	cadenceID flow.Identifier,
	block *models.Block,
	batch *pebbleDB.Batch,
) error {
	if block == nil { // safety check shouldn't happen
		return fmt.Errorf("can't process empty EVM block for Flow block: %d", cadenceHeight)
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
	e.log.Info().
		Str("hash", blockHash.Hex()).
		Uint64("evm-height", block.Height).
		Uint64("cadence-height", cadenceHeight).
		Str("cadence-id", cadenceID.String()).
		Str("parent-hash", block.ParentBlockHash.String()).
		Str("tx-hashes-root", block.TransactionHashRoot.String()).
		Msg("new evm block executed event")

	return e.blocks.Store(cadenceHeight, cadenceID, block, batch)
}

func (e *Engine) indexTransaction(
	tx models.Transaction,
	receipt *models.Receipt,
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
		return fmt.Errorf("failed to store tx: %s, with: %w", tx.Hash(), err)
	}

	return nil
}

func (e *Engine) indexReceipts(
	receipts []*models.Receipt,
	batch *pebbleDB.Batch,
) error {
	if receipts == nil {
		return nil
	}

	if err := e.receipts.Store(receipts, batch); err != nil {
		return fmt.Errorf("failed to store receipt: %w", err)
	}

	return nil
}

func registerEntriesFromKeyValue(keyValue map[flowGo.RegisterID]flowGo.RegisterValue) []flowGo.RegisterEntry {
	entries := make([]flowGo.RegisterEntry, 0, len(keyValue))
	for k, v := range keyValue {
		entries = append(entries, flowGo.RegisterEntry{
			Key:   k,
			Value: v,
		})
	}
	return entries
}
