package ingestion

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	flowGo "github.com/onflow/flow-go/model/flow"

	pebbleDB "github.com/cockroachdb/pebble"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/replayer"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-go/fvm/evm/offchain/sync"
	"math/big"
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
	userOps         storage.UserOperationIndexer
	requester       requester.Requester
	entryPointAddr  common.Address
	log             zerolog.Logger
	evmLastHeight   *models.SequentialHeight
	blocksPublisher *models.Publisher[*models.Block]
	logsPublisher   *models.Publisher[[]*gethTypes.Log]
	collector       metrics.Collector
	replayerConfig  replayer.Config
	evmChainID      *big.Int
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
	userOps storage.UserOperationIndexer,
	requester requester.Requester,
	entryPointAddr common.Address,
	blocksPublisher *models.Publisher[*models.Block],
	logsPublisher *models.Publisher[[]*gethTypes.Log],
	log zerolog.Logger,
	collector metrics.Collector,
	replayerConfig replayer.Config,
	evmChainID *big.Int,
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
		userOps:         userOps,
		requester:       requester,
		entryPointAddr:  entryPointAddr,
		log:             log,
		blocksPublisher: blocksPublisher,
		logsPublisher:   logsPublisher,
		collector:       collector,
		replayerConfig:  replayerConfig,
		evmChainID:      evmChainID,
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
		return err
	}

	// Index UserOperation events if UserOps storage is available
	if e.userOps != nil && e.entryPointAddr != (common.Address{}) {
		block := events.Block()
		if err := e.indexUserOperationEvents(block, events.Transactions(), events.Receipts(), batch); err != nil {
			e.log.Warn().Err(err).Msg("failed to index user operation events")
			// Don't fail the entire block indexing if UserOp indexing fails
		}
	}
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

// indexUserOperationEvents indexes UserOperation events from EntryPoint logs
func (e *Engine) indexUserOperationEvents(
	block *models.Block,
	transactions []models.Transaction,
	receipts []*models.Receipt,
	batch *pebbleDB.Batch,
) error {
	if e.userOps == nil || e.entryPointAddr == (common.Address{}) {
		return nil
	}

	blockHash, err := block.Hash()
	if err != nil {
		return fmt.Errorf("failed to get block hash: %w", err)
	}

	// Create a map of transaction hash to transaction for quick lookup
	txMap := make(map[common.Hash]models.Transaction)
	for _, tx := range transactions {
		txMap[tx.Hash()] = tx
	}

	// Iterate through all receipts and find EntryPoint transactions
	for _, receipt := range receipts {
		tx, ok := txMap[receipt.TxHash]
		if !ok {
			continue
		}

		// Check if transaction targets EntryPoint
		to := tx.To()
		if to == nil || *to != e.entryPointAddr {
			continue
		}

		// Decode UserOps from calldata to verify expected count
		calldata := tx.Data()
		expectedUserOps, beneficiary, decodeErr := requester.DecodeHandleOps(calldata)
		expectedUserOpCount := 0
		if decodeErr == nil {
			expectedUserOpCount = len(expectedUserOps)
		}

		// Check if transaction failed (status 0x0) with no logs
		// This indicates EntryPoint.handleOps() reverted before emitting events
		if receipt.Status == 0 && len(receipt.Logs) == 0 {
			if decodeErr != nil {
				// Log comprehensive error with calldata hex for production debugging
				e.log.Error().
					Err(decodeErr).
					Str("txHash", receipt.TxHash.Hex()).
					Str("entryPoint", e.entryPointAddr.Hex()).
					Int("calldataLen", len(calldata)).
					Str("calldataHex", hexutil.Encode(calldata)).
					Str("revertReason", e.parseRevertReason(receipt.RevertReason)).
					Str("revertReasonHex", hexutil.Encode(receipt.RevertReason)).
					Int("receiptStatus", int(receipt.Status)).
					Int("logCount", len(receipt.Logs)).
					Msg("failed to decode handleOps calldata from failed transaction - cannot index UserOps. This prevents proper error diagnostics.")
				continue
			}

			// Parse revert reason to extract error message
			revertReasonStr := e.parseRevertReason(receipt.RevertReason)

			// Process each UserOp in the batch
			for i, userOp := range expectedUserOps {
				// MUST use EntryPoint.getUserOpHash() for authoritative hash - NO FALLBACKS
				height, err := e.blocks.LatestEVMHeight()
				if err != nil {
					e.log.Warn().
						Err(err).
						Str("txHash", receipt.TxHash.Hex()).
						Int("opIndex", i).
						Str("sender", userOp.Sender.Hex()).
						Msg("failed to get latest height for EntryPoint.getUserOpHash()")
					continue
				}
				userOpHash, err := e.requester.GetUserOpHash(context.Background(), userOp, e.entryPointAddr, height)
				if err != nil {
					e.log.Warn().
						Err(err).
						Str("txHash", receipt.TxHash.Hex()).
						Int("opIndex", i).
						Str("sender", userOp.Sender.Hex()).
						Msg("failed to get UserOp hash from EntryPoint.getUserOpHash()")
					continue
				}

				// Enhanced diagnostics for failed UserOps
				// Extract AA error code if present
				aaErrorCode := e.extractAAErrorCode(revertReasonStr)
				
				// Try to extract inner revert reason if this is FailedOpWithRevert
				innerRevertReason := ""
				innerErrorSelector := ""
				if strings.Contains(revertReasonStr, "FailedOpWithRevert") && len(receipt.RevertReason) > 0 {
					// Always try to extract error selector first (even if inner reason is empty)
					// The inner revert data might be just a selector without a string message
					if len(receipt.RevertReason) >= 4 {
						innerSelector := e.extractErrorSelectorFromFailedOpWithRevert(receipt.RevertReason)
						if innerSelector != "" {
							innerErrorSelector = innerSelector
						}
					}
					
					// Try to extract inner revert data from FailedOpWithRevert
					innerReason := e.extractInnerRevertFromFailedOpWithRevert(receipt.RevertReason)
					if innerReason != "" {
						innerRevertReason = innerReason
					}
				}
				
				// Log comprehensive diagnostics for all AA error codes
				e.logAAErrorDiagnostics(userOp, userOpHash, receipt, revertReasonStr, aaErrorCode, innerRevertReason, innerErrorSelector, i, beneficiary)

				// Store UserOperation receipt with failure
				userOpReceipt := &storage.UserOperationReceipt{
					UserOpHash:  userOpHash,
					EntryPoint:  e.entryPointAddr,
					Sender:      userOp.Sender,
					Nonce:       userOp.Nonce,
					Success:     false,
					Reason:      revertReasonStr,
					TxHash:      receipt.TxHash,
					BlockNumber: big.NewInt(int64(block.Height)),
					BlockHash:   blockHash,
				}

				if err := e.userOps.StoreUserOpReceipt(userOpHash, userOpReceipt, batch); err != nil {
					e.log.Error().
						Err(err).
						Str("userOpHash", userOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", userOp.Sender.Hex()).
						Str("nonce", userOp.Nonce.String()).
						Str("aaErrorCode", aaErrorCode).
						Str("reason", revertReasonStr).
						Int("opIndex", i).
						Int("blockHeight", int(block.Height)).
						Msg("failed to store UserOperation receipt for failed transaction - receipt data will be lost")
					continue
				}

				if err := e.userOps.StoreUserOpTxMapping(userOpHash, receipt.TxHash, batch); err != nil {
					e.log.Error().
						Err(err).
						Str("userOpHash", userOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", userOp.Sender.Hex()).
						Int("opIndex", i).
						Int("blockHeight", int(block.Height)).
						Msg("failed to store UserOperation tx mapping for failed transaction - mapping data will be lost")
					continue
				}
			}
		}

		// Track processed UserOps to detect missing events
		processedUserOpHashes := make(map[common.Hash]bool)
		entryPointLogCount := 0
		userOpEventCount := 0
		userOpRevertCount := 0

		// Parse logs for UserOperation events
		for _, log := range receipt.Logs {
			// Check if log is from EntryPoint
			if log.Address != e.entryPointAddr {
				continue
			}

			entryPointLogCount++

			// Check for UserOperationEvent
			if len(log.Topics) > 0 && log.Topics[0] == UserOperationEventSig {
				userOpEventCount++
				event, err := ParseUserOperationEvent(log)
				if err != nil {
					e.log.Warn().
						Err(err).
						Str("txHash", receipt.TxHash.Hex()).
						Int("logIndex", len(processedUserOpHashes)).
						Msg("failed to parse UserOperationEvent")
					continue
				}

				processedUserOpHashes[event.UserOpHash] = true

				// Store UserOperation receipt
				userOpReceipt := &storage.UserOperationReceipt{
					UserOpHash:    event.UserOpHash,
					EntryPoint:    e.entryPointAddr,
					Sender:        event.Sender,
					Nonce:         event.Nonce,
					ActualGasCost: event.ActualGasCost,
					ActualGasUsed: event.ActualGasUsed,
					Success:       event.Success,
					TxHash:        receipt.TxHash,
					BlockNumber:   big.NewInt(int64(block.Height)),
					BlockHash:     blockHash,
				}

				if event.Paymaster != (common.Address{}) {
					userOpReceipt.Paymaster = &event.Paymaster
				}

				if err := e.userOps.StoreUserOpReceipt(event.UserOpHash, userOpReceipt, batch); err != nil {
					e.log.Error().
						Err(err).
						Str("userOpHash", event.UserOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", event.Sender.Hex()).
						Str("nonce", event.Nonce.String()).
						Bool("success", event.Success).
						Int("blockHeight", int(block.Height)).
						Msg("failed to store UserOperation receipt - receipt data will be lost")
					continue
				}

				// Store mapping from userOpHash to transaction hash
				if err := e.userOps.StoreUserOpTxMapping(event.UserOpHash, receipt.TxHash, batch); err != nil {
					e.log.Error().
						Err(err).
						Str("userOpHash", event.UserOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", event.Sender.Hex()).
						Int("blockHeight", int(block.Height)).
						Msg("failed to store UserOperation tx mapping - mapping data will be lost")
					continue
				}

				e.log.Debug().
					Str("userOpHash", event.UserOpHash.Hex()).
					Str("txHash", receipt.TxHash.Hex()).
					Str("sender", event.Sender.Hex()).
					Bool("success", event.Success).
					Msg("indexed UserOperation event")
			}

			// Check for UserOperationRevertReason
			if len(log.Topics) > 0 && log.Topics[0] == UserOperationRevertReasonSig {
				userOpRevertCount++
				revertReason, err := ParseUserOperationRevertReason(log)
				if err != nil {
					e.log.Warn().
						Err(err).
						Str("txHash", receipt.TxHash.Hex()).
						Int("logIndex", len(processedUserOpHashes)).
						Msg("failed to parse UserOperationRevertReason")
					continue
				}

				processedUserOpHashes[revertReason.UserOpHash] = true

				// Find the matching UserOp from decoded calldata for comprehensive diagnostics
				var matchingUserOp *models.UserOperation
				var opIndex int = -1
				if decodeErr == nil {
					// MUST use EntryPoint.getUserOpHash() for authoritative hash - NO FALLBACKS
					height, heightErr := e.blocks.LatestEVMHeight()
					if heightErr == nil {
						for i, userOp := range expectedUserOps {
							userOpHash, err := e.requester.GetUserOpHash(context.Background(), userOp, e.entryPointAddr, height)
							if err == nil && userOpHash == revertReason.UserOpHash {
								matchingUserOp = userOp
								opIndex = i
								break
							}
						}
					}
				}

				// Extract AA error code and inner revert information
				aaErrorCode := e.extractAAErrorCode(revertReason.RevertReason)
				innerRevertReason := ""
				innerErrorSelector := ""
				// Extract inner revert details from transaction's RevertReason (which contains FailedOpWithRevert data)
				// Note: For UserOperationRevertReason events, the transaction succeeded (status 1), so receipt.RevertReason
				// is typically empty. However, we check it anyway in case the transaction actually failed (status 0).
				if len(receipt.RevertReason) > 0 {
					// Check if this is FailedOpWithRevert by looking at the selector (first 4 bytes)
					if len(receipt.RevertReason) >= 4 {
						selector := hexutil.Encode(receipt.RevertReason[:4])
						// FailedOpWithRevert selector: keccak256("FailedOpWithRevert(uint256,string,bytes)")[:4] = 0x65c8fd4d
						failedOpWithRevertSelector := hexutil.Encode(crypto.Keccak256([]byte("FailedOpWithRevert(uint256,string,bytes)"))[:4])
						
						// Log selector comparison for debugging
						e.log.Info().
							Str("txHash", receipt.TxHash.Hex()).
							Str("selector", selector).
							Str("failedOpWithRevertSelector", failedOpWithRevertSelector).
							Bool("selectorsMatch", selector == failedOpWithRevertSelector).
							Int("revertReasonLen", len(receipt.RevertReason)).
							Int("receiptStatus", int(receipt.Status)).
							Msg("checking if revertReason is FailedOpWithRevert")
						
						if selector == failedOpWithRevertSelector {
							// Extract inner error selector first (most important for diagnostics)
							innerSelector := e.extractErrorSelectorFromFailedOpWithRevert(receipt.RevertReason)
							if innerSelector != "" {
								innerErrorSelector = innerSelector
								e.log.Info().
									Str("txHash", receipt.TxHash.Hex()).
									Str("innerErrorSelector", innerSelector).
									Int("receiptStatus", int(receipt.Status)).
									Msg("successfully extracted innerErrorSelector from FailedOpWithRevert")
							} else {
								// Info level so it shows up - log why extraction failed
								e.log.Info().
									Str("txHash", receipt.TxHash.Hex()).
									Int("revertReasonLen", len(receipt.RevertReason)).
									Str("revertReasonHex", hexutil.Encode(receipt.RevertReason)).
									Int("receiptStatus", int(receipt.Status)).
									Msg("failed to extract innerErrorSelector from FailedOpWithRevert - checking hex data")
							}
							
							// Extract inner revert reason (may be empty if only selector is present)
							innerReason := e.extractInnerRevertFromFailedOpWithRevert(receipt.RevertReason)
							if innerReason != "" {
								innerRevertReason = innerReason
							}
						}
					}
				} else {
					// Info level so it shows up - receipt.RevertReason is empty (transaction succeeded but UserOp failed)
					e.log.Info().
						Str("txHash", receipt.TxHash.Hex()).
						Int("receiptStatus", int(receipt.Status)).
						Str("eventRevertReason", revertReason.RevertReason).
						Msg("receipt.RevertReason is empty - transaction succeeded but UserOp failed via event. Cannot extract inner error selector from receipt.")
				}

				// Log comprehensive diagnostics if we have the full UserOp
				if matchingUserOp != nil {
					e.logAAErrorDiagnostics(matchingUserOp, revertReason.UserOpHash, receipt, revertReason.RevertReason, aaErrorCode, innerRevertReason, innerErrorSelector, opIndex, beneficiary)
				} else {
					// Fallback: log basic diagnostics without full UserOp details
					// Include calldata hex for debugging why decode failed
					e.log.Error().
						Str("userOpHash", revertReason.UserOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", revertReason.Sender.Hex()).
						Str("nonce", revertReason.Nonce.String()).
						Str("reason", revertReason.RevertReason).
						Str("aaErrorCode", aaErrorCode).
						Str("entryPoint", e.entryPointAddr.Hex()).
						Int("calldataLen", len(calldata)).
						Str("calldataHex", hexutil.Encode(calldata)).
						Int("receiptStatus", int(receipt.Status)).
						Int("logCount", len(receipt.Logs)).
						Msg("UserOperation failed - could not decode UserOp from calldata for full diagnostics. This prevents comprehensive error analysis.")
				}

				// Store UserOperation receipt with failure
				userOpReceipt := &storage.UserOperationReceipt{
					UserOpHash:  revertReason.UserOpHash,
					EntryPoint:  e.entryPointAddr,
					Sender:      revertReason.Sender,
					Nonce:       revertReason.Nonce,
					Success:     false,
					Reason:       revertReason.RevertReason,
					TxHash:       receipt.TxHash,
					BlockNumber:  big.NewInt(int64(block.Height)),
					BlockHash:    blockHash,
				}

				if err := e.userOps.StoreUserOpReceipt(revertReason.UserOpHash, userOpReceipt, batch); err != nil {
					e.log.Error().
						Err(err).
						Str("userOpHash", revertReason.UserOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", revertReason.Sender.Hex()).
						Str("nonce", revertReason.Nonce.String()).
						Str("aaErrorCode", aaErrorCode).
						Str("reason", revertReason.RevertReason).
						Int("blockHeight", int(block.Height)).
						Msg("failed to store UserOperation receipt - receipt data will be lost")
					continue
				}

				if err := e.userOps.StoreUserOpTxMapping(revertReason.UserOpHash, receipt.TxHash, batch); err != nil {
					e.log.Error().
						Err(err).
						Str("userOpHash", revertReason.UserOpHash.Hex()).
						Str("txHash", receipt.TxHash.Hex()).
						Str("sender", revertReason.Sender.Hex()).
						Int("blockHeight", int(block.Height)).
						Msg("failed to store UserOperation tx mapping - mapping data will be lost")
					continue
				}

				e.log.Debug().
					Str("userOpHash", revertReason.UserOpHash.Hex()).
					Str("txHash", receipt.TxHash.Hex()).
					Str("reason", revertReason.RevertReason).
					Msg("indexed UserOperation revert reason")
			}
		}

		// Validate that we processed all expected UserOps
		processedCount := len(processedUserOpHashes)
		if receipt.Status == 1 {
			// Transaction succeeded - verify we got events for all UserOps
			if decodeErr == nil && expectedUserOpCount > 0 {
				if processedCount == 0 {
					// Transaction succeeded but no UserOp events - this is unexpected
					e.log.Warn().
						Str("txHash", receipt.TxHash.Hex()).
						Int("expectedUserOpCount", expectedUserOpCount).
						Int("processedCount", processedCount).
						Int("entryPointLogCount", entryPointLogCount).
						Int("totalLogCount", len(receipt.Logs)).
						Str("beneficiary", beneficiary.Hex()).
						Msg("EntryPoint transaction succeeded but no UserOperation events found - this may indicate EntryPoint didn't process any UserOps or events were not emitted")
				} else if processedCount < expectedUserOpCount {
					// Some UserOps missing events - partial failure scenario
					e.log.Warn().
						Str("txHash", receipt.TxHash.Hex()).
						Int("expectedUserOpCount", expectedUserOpCount).
						Int("processedCount", processedCount).
						Int("missingCount", expectedUserOpCount-processedCount).
						Int("userOpEventCount", userOpEventCount).
						Int("userOpRevertCount", userOpRevertCount).
						Str("beneficiary", beneficiary.Hex()).
						Msg("EntryPoint transaction succeeded but some UserOps are missing events - some UserOps may have failed silently or events were not emitted")
					
					// Try to identify which UserOps are missing
					if len(expectedUserOps) > 0 {
						// MUST use EntryPoint.getUserOpHash() for authoritative hash - NO FALLBACKS
						height, heightErr := e.blocks.LatestEVMHeight()
						if heightErr == nil {
							for i, userOp := range expectedUserOps {
								userOpHash, err := e.requester.GetUserOpHash(context.Background(), userOp, e.entryPointAddr, height)
								if err == nil && !processedUserOpHashes[userOpHash] {
									e.log.Warn().
										Str("txHash", receipt.TxHash.Hex()).
										Str("userOpHash", userOpHash.Hex()).
										Str("sender", userOp.Sender.Hex()).
										Int("opIndex", i).
										Msg("UserOp missing from events - may have failed or event not emitted")
								}
							}
						}
					}
				} else if processedCount > expectedUserOpCount {
					// More events than expected - shouldn't happen but log it
					e.log.Warn().
						Str("txHash", receipt.TxHash.Hex()).
						Int("expectedUserOpCount", expectedUserOpCount).
						Int("processedCount", processedCount).
						Msg("EntryPoint transaction has more UserOperation events than UserOps in calldata - this may indicate duplicate events or calldata decode issue")
				}
			} else if decodeErr != nil && processedCount > 0 {
				// Couldn't decode calldata but got events - log for investigation
				e.log.Info().
					Err(decodeErr).
					Str("txHash", receipt.TxHash.Hex()).
					Int("processedCount", processedCount).
					Int("calldataLen", len(calldata)).
					Msg("EntryPoint transaction succeeded with UserOp events but calldata decode failed - events processed successfully")
			}
		} else if receipt.Status == 0 && len(receipt.Logs) > 0 {
			// Transaction failed but has logs - this is unusual
			e.log.Warn().
				Str("txHash", receipt.TxHash.Hex()).
				Int("logCount", len(receipt.Logs)).
				Int("entryPointLogCount", entryPointLogCount).
				Int("processedCount", processedCount).
				Str("revertReason", e.parseRevertReason(receipt.RevertReason)).
				Msg("EntryPoint transaction failed (status 0) but has logs - this may indicate partial execution or unexpected revert")
		} else if receipt.Status == 1 && entryPointLogCount > 0 && processedCount == 0 {
			// Transaction succeeded, has EntryPoint logs, but no UserOp events
			e.log.Warn().
				Str("txHash", receipt.TxHash.Hex()).
				Int("entryPointLogCount", entryPointLogCount).
				Int("totalLogCount", len(receipt.Logs)).
				Msg("EntryPoint transaction succeeded with EntryPoint logs but no UserOperation events - EntryPoint may have emitted other events")
		}
	}

	return nil
}

// parseRevertReason extracts the error message from revert reason bytes
// Handles various formats: Error(string), FailedOp, FailedOpWithRevert, and custom errors
func (e *Engine) parseRevertReason(revertData []byte) string {
	if len(revertData) == 0 {
		return "Transaction reverted (no reason provided)"
	}

	// Try to decode as Error(string) - selector 0x08c379a0
	if len(revertData) >= 4 {
		errorSelector := hexutil.Encode(revertData[:4])
		if errorSelector == "0x08c379a0" && len(revertData) >= 68 {
			// Error(string) format: selector (4) + offset (32) + length (32) + string data
			offset := new(big.Int).SetBytes(revertData[4:36])
			if offset.Cmp(big.NewInt(32)) == 0 {
				strLen := new(big.Int).SetBytes(revertData[36:68])
				if strLen.Cmp(big.NewInt(0)) > 0 {
					strLenInt := int(strLen.Int64())
					if len(revertData) >= 68+strLenInt {
						strBytes := revertData[68 : 68+strLenInt]
						// Remove null padding
						for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
							strBytes = strBytes[:len(strBytes)-1]
						}
						if len(strBytes) > 0 {
							return string(strBytes)
						}
					}
				}
			}
		}

		// Try to decode as FailedOp(uint256,string) - check selector from ABI
		// FailedOp selector is 0x220266b6
		if errorSelector == "0x220266b6" && len(revertData) >= 100 {
			opIndex := new(big.Int).SetBytes(revertData[4:36])
			offset := new(big.Int).SetBytes(revertData[36:68])
			if offset.Cmp(big.NewInt(64)) == 0 {
				strLen := new(big.Int).SetBytes(revertData[68:100])
				if strLen.Cmp(big.NewInt(0)) > 0 {
					strLenInt := int(strLen.Int64())
					if len(revertData) >= 100+strLenInt {
						strBytes := revertData[100 : 100+strLenInt]
						// Remove null padding
						for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
							strBytes = strBytes[:len(strBytes)-1]
						}
						if len(strBytes) > 0 {
							reason := string(strBytes)
							return fmt.Sprintf("FailedOp(opIndex=%s, reason=%q)", opIndex.String(), reason)
						}
					}
				}
			}
		}

		// Try to decode as FailedOpWithRevert(uint256,string,bytes) - contains inner revert data
		// FailedOpWithRevert selector: keccak256("FailedOpWithRevert(uint256,string,bytes)")[:4]
		failedOpWithRevertSelectorBytes := crypto.Keccak256([]byte("FailedOpWithRevert(uint256,string,bytes)"))[:4]
		failedOpWithRevertSelector := hexutil.Encode(failedOpWithRevertSelectorBytes)
		if errorSelector == failedOpWithRevertSelector && len(revertData) >= 100 {
			opIndex := new(big.Int).SetBytes(revertData[4:36])
			offset := new(big.Int).SetBytes(revertData[36:68])
			
			// For FailedOpWithRevert, offset should be 96 (0x60) to skip opIndex and offsets
			if offset.Cmp(big.NewInt(96)) == 0 && len(revertData) >= 132 {
				strLen := new(big.Int).SetBytes(revertData[100:132])
				if strLen.Cmp(big.NewInt(0)) > 0 {
					strLenInt := int(strLen.Int64())
					if len(revertData) >= 132+strLenInt {
						strBytes := revertData[132 : 132+strLenInt]
						// Remove null padding
						for len(strBytes) > 0 && strBytes[len(strBytes)-1] == 0 {
							strBytes = strBytes[:len(strBytes)-1]
						}
						if len(strBytes) > 0 {
							reason := string(strBytes)
							
							// Try to decode inner revert data (bytes field)
							bytesOffset := 132 + strLenInt
							// Align to 32-byte boundary for next field
							bytesOffset = ((bytesOffset + 31) / 32) * 32
							if len(revertData) >= bytesOffset+32 {
								bytesLen := new(big.Int).SetBytes(revertData[bytesOffset : bytesOffset+32])
								if bytesLen.Cmp(big.NewInt(0)) > 0 {
									bytesLenInt := int(bytesLen.Int64())
									if len(revertData) >= bytesOffset+32+bytesLenInt {
										innerRevertData := revertData[bytesOffset+32 : bytesOffset+32+bytesLenInt]
										innerReason := e.parseRevertReason(innerRevertData) // Recursive call
										return fmt.Sprintf("FailedOpWithRevert(opIndex=%s, reason=%q, innerRevert=%q)", opIndex.String(), reason, innerReason)
									}
								}
							}
							return fmt.Sprintf("FailedOpWithRevert(opIndex=%s, reason=%q)", opIndex.String(), reason)
						}
					}
				}
			}
		}

		// Try to extract ASCII string from revert data (for custom errors)
		// Look for printable ASCII characters
		if len(revertData) > 4 {
			// Skip selector and try to find ASCII string
			dataAfterSelector := revertData[4:]
			var asciiBytes []byte
			for _, b := range dataAfterSelector {
				if b >= 32 && b < 127 {
					asciiBytes = append(asciiBytes, b)
				} else if len(asciiBytes) > 0 {
					// Found some ASCII, check if it's meaningful
					if len(asciiBytes) >= 4 {
						reason := string(asciiBytes)
						// Remove null bytes and trim
						reason = strings.Trim(reason, "\x00")
						if len(reason) > 0 {
							return reason
						}
					}
					asciiBytes = nil
				}
			}
			if len(asciiBytes) >= 4 {
				reason := string(asciiBytes)
				reason = strings.Trim(reason, "\x00")
				if len(reason) > 0 {
					return reason
				}
			}
		}
	}

	// Fallback: return hex representation with attempt to extract AA error code
	hexReason := hexutil.Encode(revertData)
	aaErrorCode := e.extractAAErrorCodeFromHex(hexReason)
	if aaErrorCode != "" {
		return fmt.Sprintf("%s (raw revert data: %s)", aaErrorCode, hexReason)
	}
	return fmt.Sprintf("Transaction reverted (reason: %s)", hexReason)
}

// extractAAErrorCode extracts AAxx error code from a decoded error message
func (e *Engine) extractAAErrorCode(message string) string {
	// Look for AA followed by digits (e.g., "AA13", "AA20", "AA23")
	for i := 0; i < len(message)-3; i++ {
		if message[i] == 'A' && message[i+1] == 'A' {
			if message[i+2] >= '0' && message[i+2] <= '9' && message[i+3] >= '0' && message[i+3] <= '9' {
				return message[i : i+4]
			}
		}
	}
	return ""
}

// extractAAErrorCodeFromHex extracts AAxx error code from hex-encoded revert data
func (e *Engine) extractAAErrorCodeFromHex(hexData string) string {
	// Try to find "AA" followed by two digits in the hex string
	// Convert hex to string and search
	if len(hexData) >= 4 {
		// Skip "0x" prefix if present
		data := hexData
		if strings.HasPrefix(data, "0x") {
			data = data[2:]
		}
		// Try to decode as ASCII
		bytes, err := hexutil.Decode("0x" + data)
		if err == nil {
			asciiStr := string(bytes)
			// Look for AA followed by digits
			for i := 0; i < len(asciiStr)-3; i++ {
				if asciiStr[i] == 'A' && asciiStr[i+1] == 'A' {
					if asciiStr[i+2] >= '0' && asciiStr[i+2] <= '9' && asciiStr[i+3] >= '0' && asciiStr[i+3] <= '9' {
						return asciiStr[i : i+4]
					}
				}
			}
		}
	}
	return ""
}

// extractInnerRevertFromFailedOpWithRevert extracts the inner revert reason from FailedOpWithRevert error data
func (e *Engine) extractInnerRevertFromFailedOpWithRevert(revertData []byte) string {
	if len(revertData) < 100 {
		return ""
	}
	
	// FailedOpWithRevert format: selector (4) + opIndex (32) + string offset (32) + bytes offset (32) + string length (32) + string data + bytes length (32) + bytes data
	offset := new(big.Int).SetBytes(revertData[36:68])
	if offset.Cmp(big.NewInt(96)) == 0 && len(revertData) >= 132 {
		strLen := new(big.Int).SetBytes(revertData[100:132])
		if strLen.Cmp(big.NewInt(0)) > 0 {
			strLenInt := int(strLen.Int64())
			if len(revertData) >= 132+strLenInt {
				// Skip string data and get bytes field
				bytesOffset := 132 + strLenInt
				// Align to 32-byte boundary
				bytesOffset = ((bytesOffset + 31) / 32) * 32
				if len(revertData) >= bytesOffset+32 {
					bytesLen := new(big.Int).SetBytes(revertData[bytesOffset : bytesOffset+32])
					if bytesLen.Cmp(big.NewInt(0)) > 0 {
						bytesLenInt := int(bytesLen.Int64())
						if len(revertData) >= bytesOffset+32+bytesLenInt {
							innerRevertData := revertData[bytesOffset+32 : bytesOffset+32+bytesLenInt]
							return e.parseRevertReason(innerRevertData)
						}
					}
				}
			}
		}
	}
	return ""
}

// extractErrorSelectorFromFailedOpWithRevert extracts the error selector from the inner revert data in FailedOpWithRevert
func (e *Engine) extractErrorSelectorFromFailedOpWithRevert(revertData []byte) string {
	// Always log entry to confirm function is being called
	e.log.Info().
		Int("revertDataLen", len(revertData)).
		Str("revertDataHex", hexutil.Encode(revertData)).
		Msg("extractErrorSelectorFromFailedOpWithRevert: called - starting extraction")
	
	if len(revertData) < 100 {
		e.log.Info().
			Int("revertDataLen", len(revertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: revertData too short (< 100 bytes)")
		return ""
	}
	
	// FailedOpWithRevert(uint256,string,bytes) format:
	// - selector (4 bytes)
	// - opIndex (32 bytes, offset 4-36)
	// - offset to reason string (32 bytes, offset 36-68) = should be 96
	// - offset to bytes field (32 bytes, offset 68-100) = should be 160
	// - reason string length (32 bytes, offset 100-132)
	
	// - reason string data (variable, offset 132+)
	// - bytes length (32 bytes, at offset specified by bytes offset)
	// - bytes data (variable, after bytes length)
	
	// Check offset to reason string (should be 96)
	reasonOffset := new(big.Int).SetBytes(revertData[36:68])
	if reasonOffset.Cmp(big.NewInt(96)) != 0 {
		e.log.Info().
			Str("reasonOffset", reasonOffset.String()).
			Str("expectedOffset", "96").
			Int("revertDataLen", len(revertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: reasonOffset != 96")
		return ""
	}
	if len(revertData) < 100 {
		e.log.Info().
			Int("revertDataLen", len(revertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: revertData too short after reasonOffset check")
		return ""
	}
	
	// Get offset to bytes field (should be 160, but may vary based on reason string length)
	bytesOffsetPtr := new(big.Int).SetBytes(revertData[68:100])
	if bytesOffsetPtr.Cmp(big.NewInt(0)) == 0 {
		e.log.Info().
			Str("bytesOffsetPtr", bytesOffsetPtr.String()).
			Msg("extractErrorSelectorFromFailedOpWithRevert: bytesOffsetPtr is zero")
		return ""
	}
	bytesOffsetInt := int(bytesOffsetPtr.Int64())
	
	// Log the reason string details to understand the structure
	reasonLenBytes := revertData[100:132]
	reasonLen := new(big.Int).SetBytes(reasonLenBytes)
	reasonLenInt := int(reasonLen.Int64())
	reasonStart := 132
	reasonEnd := reasonStart + reasonLenInt
	reasonPaddedEnd := reasonStart + ((reasonLenInt + 31) / 32) * 32 // Round up to 32-byte boundary
	
	e.log.Info().
		Int("reasonLenInt", reasonLenInt).
		Int("reasonStart", reasonStart).
		Int("reasonEnd", reasonEnd).
		Int("reasonPaddedEnd", reasonPaddedEnd).
		Int("bytesOffsetInt", bytesOffsetInt).
		Str("reasonString", string(revertData[reasonStart:reasonEnd])).
		Msg("extractErrorSelectorFromFailedOpWithRevert: reason string details")
	
	// Validate bytes offset is reasonable and we have enough data
	// The offset should be at least 100 (after opIndex and offsets) and less than data length
	if bytesOffsetInt < 100 {
		e.log.Info().
			Int("bytesOffsetInt", bytesOffsetInt).
			Int("revertDataLen", len(revertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: bytesOffsetInt < 100")
		return ""
	}
	if bytesOffsetInt >= len(revertData) {
		e.log.Info().
			Int("bytesOffsetInt", bytesOffsetInt).
			Int("revertDataLen", len(revertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: bytesOffsetInt >= revertDataLen")
		return ""
	}
	
	// We need at least 32 bytes at the offset to read the bytes length
	if len(revertData) < bytesOffsetInt+32 {
		e.log.Info().
			Int("bytesOffsetInt", bytesOffsetInt).
			Int("revertDataLen", len(revertData)).
			Int("requiredLen", bytesOffsetInt+32).
			Msg("extractErrorSelectorFromFailedOpWithRevert: not enough data for bytes length word")
		return ""
	}
	
	// The bytes offset pointer might point to padding instead of the actual bytes field
	// If the expected offset (from reason string padding) differs, use that instead
	actualBytesOffset := bytesOffsetInt
	if reasonPaddedEnd != bytesOffsetInt && reasonPaddedEnd > 0 && reasonPaddedEnd < len(revertData) {
		// Check if the expected offset has non-zero data (the bytes length)
		expectedBytesLenBytes := revertData[reasonPaddedEnd : reasonPaddedEnd+32]
		expectedBytesLen := new(big.Int).SetBytes(expectedBytesLenBytes)
		if expectedBytesLen.Cmp(big.NewInt(0)) > 0 {
			e.log.Info().
				Int("bytesOffsetInt", bytesOffsetInt).
				Int("reasonPaddedEnd", reasonPaddedEnd).
				Str("expectedBytesLen", expectedBytesLen.String()).
				Msg("extractErrorSelectorFromFailedOpWithRevert: using expected offset (reasonPaddedEnd) instead of bytesOffsetInt")
			actualBytesOffset = reasonPaddedEnd
		}
	}
	
	// Read bytes length (32-byte word at actualBytesOffset)
	// But first, check if we have enough data
	if actualBytesOffset+32 > len(revertData) {
		e.log.Info().
			Int("actualBytesOffset", actualBytesOffset).
			Int("revertDataLen", len(revertData)).
			Int("requiredLen", actualBytesOffset+32).
			Msg("extractErrorSelectorFromFailedOpWithRevert: not enough data for bytes length field")
		return ""
	}
	
	bytesLenBytes := revertData[actualBytesOffset : actualBytesOffset+32]
	
	// Log the exact bytes we're reading
	e.log.Info().
		Int("actualBytesOffset", actualBytesOffset).
		Int("bytesOffsetInt", bytesOffsetInt).
		Int("sliceStart", actualBytesOffset).
		Int("sliceEnd", actualBytesOffset+32).
		Int("revertDataLen", len(revertData)).
		Str("bytesLenHex", hexutil.Encode(bytesLenBytes)).
		Str("bytesLenHexRaw", fmt.Sprintf("%x", bytesLenBytes)).
		Int("bytesLenBytesLen", len(bytesLenBytes)).
		Msg("extractErrorSelectorFromFailedOpWithRevert: raw bytes at offset")
	
	bytesLen := new(big.Int).SetBytes(bytesLenBytes)
	
	// Also try reading it as a uint64 to see if there's an endianness issue
	var bytesLenUint64 uint64
	if len(bytesLenBytes) >= 8 {
		bytesLenUint64 = binary.BigEndian.Uint64(bytesLenBytes[24:32]) // Last 8 bytes
	}
	e.log.Info().
		Str("bytesLenBigInt", bytesLen.String()).
		Uint64("bytesLenUint64", bytesLenUint64).
		Str("lastByte", hexutil.Encode(bytesLenBytes[31:32])).
		Msg("extractErrorSelectorFromFailedOpWithRevert: bytes length interpretation")
	
	
	// Check if bytesLen is actually 0 or if we're misreading it
	// Sometimes the last byte might be the actual length (for small values)
	bytesLenInt := 0
	if bytesLen.Cmp(big.NewInt(0)) == 0 {
		// Check the last byte - for small values like 4, it might be in the last byte
		lastByte := bytesLenBytes[31]
		if lastByte > 0 && lastByte <= 32 {
			e.log.Info().
				Int("actualBytesOffset", actualBytesOffset).
				Str("bytesLenBigInt", bytesLen.String()).
				Uint8("lastByte", lastByte).
				Msg("extractErrorSelectorFromFailedOpWithRevert: bytesLen is zero but last byte suggests length - using last byte as length")
			bytesLen = big.NewInt(int64(lastByte))
			bytesLenInt = int(lastByte)
		} else {
			// Check if there's data after the length field that might be the actual bytes
			if len(revertData) > actualBytesOffset+32 {
				nextBytes := revertData[actualBytesOffset+32:]
				e.log.Info().
					Int("actualBytesOffset", actualBytesOffset).
					Str("bytesLen", bytesLen.String()).
					Int("nextBytesLen", len(nextBytes)).
					Str("nextBytesHex", hexutil.Encode(nextBytes)).
					Msg("extractErrorSelectorFromFailedOpWithRevert: bytesLen is zero, but there's data after the length field - checking if it's the inner revert data")
				
				// If the length is 0 but there's data, it might be that the bytes field is just the selector (4 bytes)
				// Try to extract it directly
				if len(nextBytes) >= 4 {
					selector := hexutil.Encode(nextBytes[:4])
					e.log.Info().
						Str("extractedSelector", selector).
						Msg("extractErrorSelectorFromFailedOpWithRevert: extracted selector from data after zero length field")
					return selector
				}
			}
			e.log.Info().
				Int("actualBytesOffset", actualBytesOffset).
				Str("bytesLen", bytesLen.String()).
				Msg("extractErrorSelectorFromFailedOpWithRevert: bytesLen is zero and no data found")
			return ""
		}
	} else {
		bytesLenInt = int(bytesLen.Int64())
	}
	
	// Validate bytes length is reasonable (at least 4 bytes for selector, not too large)
	if bytesLenInt < 4 {
		e.log.Info().
			Int("bytesLenInt", bytesLenInt).
			Msg("extractErrorSelectorFromFailedOpWithRevert: bytesLenInt < 4 (need at least selector)")
		return ""
	}
	if bytesLenInt > len(revertData) {
		e.log.Info().
			Int("bytesLenInt", bytesLenInt).
			Int("revertDataLen", len(revertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: bytesLenInt > revertDataLen")
		return ""
	}
	
	// Validate we have enough data for the bytes field
	// Need: actualBytesOffset (offset) + 32 (length word) + bytesLenInt (actual bytes)
	requiredLen := actualBytesOffset + 32 + bytesLenInt
	if len(revertData) < requiredLen {
		e.log.Info().
			Int("actualBytesOffset", actualBytesOffset).
			Int("bytesLenInt", bytesLenInt).
			Int("revertDataLen", len(revertData)).
			Int("requiredLen", requiredLen).
			Msg("extractErrorSelectorFromFailedOpWithRevert: not enough data for bytes field")
		return ""
	}
	
	// Extract inner revert data (first 4 bytes are the error selector)
	innerRevertData := revertData[actualBytesOffset+32 : actualBytesOffset+32+bytesLenInt]
	if len(innerRevertData) >= 4 {
		selector := hexutil.Encode(innerRevertData[:4])
		e.log.Info().
			Str("innerErrorSelector", selector).
			Int("actualBytesOffset", actualBytesOffset).
			Int("bytesLenInt", bytesLenInt).
			Int("innerRevertDataLen", len(innerRevertData)).
			Str("innerRevertDataHex", hexutil.Encode(innerRevertData)).
			Msg("extractErrorSelectorFromFailedOpWithRevert: successfully extracted inner error selector")
		return selector
	}
	
	e.log.Info().
		Int("innerRevertDataLen", len(innerRevertData)).
		Msg("extractErrorSelectorFromFailedOpWithRevert: innerRevertData < 4 bytes (no selector)")
	return ""
}

// logAAErrorDiagnostics logs comprehensive diagnostics for all AA error codes
func (e *Engine) logAAErrorDiagnostics(userOp *models.UserOperation, userOpHash common.Hash, receipt *models.Receipt, revertReasonStr string, aaErrorCode string, innerRevertReason string, innerErrorSelector string, opIndex int, beneficiary common.Address) {
	// Info: confirm function is being called (using Info level so it shows up with --log-level=info)
	e.log.Info().
		Str("aaErrorCode", aaErrorCode).
		Str("userOpHash", userOpHash.Hex()).
		Str("component", "ingestion").
		Msg("logAAErrorDiagnostics called - logging comprehensive AA error diagnostics")
	
	// Use Warn level for all AA errors to make them more visible
	logEntry := e.log.Warn()
	
	// Add common UserOp fields
	logEntry = logEntry.
		Str("userOpHash", userOpHash.Hex()).
		// expectedUserOpHash is the same as userOpHash but logged explicitly to aid frontend/backend hash comparison
		Str("expectedUserOpHash", userOpHash.Hex()).
		Str("txHash", receipt.TxHash.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("nonce", userOp.Nonce.String()).
		Int("opIndex", opIndex).
		Str("reason", revertReasonStr).
		Str("aaErrorCode", aaErrorCode).
		Str("revertReasonHex", hexutil.Encode(receipt.RevertReason)).
		Int("revertReasonLen", len(receipt.RevertReason)).
		Int("callDataLen", len(userOp.CallData)).
		Str("callDataHex", hexutil.Encode(userOp.CallData)).
		Int("initCodeLen", len(userOp.InitCode)).
		Str("callGasLimit", userOp.CallGasLimit.String()).
		Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
		Str("preVerificationGas", userOp.PreVerificationGas.String()).
		Str("maxFeePerGas", userOp.MaxFeePerGas.String()).
		Str("maxPriorityFeePerGas", userOp.MaxPriorityFeePerGas.String()).
		Str("beneficiary", beneficiary.Hex()).
		Str("entryPoint", e.entryPointAddr.Hex()).
		Str("chainID", e.evmChainID.String())
	
	// Add inner revert information if available
	if innerRevertReason != "" {
		logEntry = logEntry.Str("innerRevertReason", innerRevertReason)
	}
	if innerErrorSelector != "" {
		logEntry = logEntry.Str("innerErrorSelector", innerErrorSelector)
	}
	
	// Extract signature details for signature-related errors
	signatureV := ""
	signatureR := ""
	signatureS := ""
	if len(userOp.Signature) >= 65 {
		v := userOp.Signature[64]
		signatureV = fmt.Sprintf("%d (0x%02x)", v, v)
		signatureR = hexutil.Encode(userOp.Signature[0:32])
		signatureS = hexutil.Encode(userOp.Signature[32:64])
	}
	
	// Extract paymaster address if present
	paymasterAddress := ""
	if len(userOp.PaymasterAndData) >= 20 {
		paymasterAddress = common.BytesToAddress(userOp.PaymasterAndData[:20]).Hex()
	}
	
	// Error-specific diagnostics and messages
	switch aaErrorCode {
	case "AA10":
		logEntry.
			Msg("AA10: account already exists - UserOp tried to create an account that already exists. Check: 1) sender address is correct, 2) account was not already created in a previous transaction")
	
	case "AA11":
		logEntry.
			Msg("AA11: account not deployed - UserOp tried to use an account that doesn't exist and no initCode was provided. Check: 1) sender address is correct, 2) account exists on-chain, 3) initCode is provided for account creation")
	
	case "AA12":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Msg("AA12: paymaster deposit too low - Paymaster doesn't have enough deposit to cover gas costs. Check: 1) paymaster address has sufficient deposit, 2) paymaster deposit is staked, 3) gas costs are within paymaster's deposit")
	
	case "AA13":
		// Check for specific inner error selectors that indicate common issues
		isAlreadyInitialized := innerErrorSelector == "0xf92ee8a9" || strings.Contains(strings.ToLower(innerRevertReason), "initialized")
		isAccountExists := strings.Contains(strings.ToLower(revertReasonStr), "account already exists") || strings.Contains(strings.ToLower(innerRevertReason), "account already exists")
		
		if isAlreadyInitialized {
			// Extract factory and account address from initCode
			factoryAddr := ""
			expectedAccountAddr := userOp.Sender.Hex()
			if len(userOp.InitCode) >= 20 {
				factoryAddr = common.BytesToAddress(userOp.InitCode[0:20]).Hex()
			}
			logEntry.
				Int("initCodeLen", len(userOp.InitCode)).
				Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
				Str("factoryAddress", factoryAddr).
				Str("expectedAccountAddress", expectedAccountAddr).
				Str("sender", userOp.Sender.Hex()).
				Str("innerErrorSelector", innerErrorSelector).
				Str("innerRevertReason", innerRevertReason).
				Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
				Msg("AA13: account already initialized (0xf92ee8a9) - The account at the expected address already exists and is initialized. This usually means: 1) Account was created in a previous transaction, 2) Salt is being reused, 3) Factory's getAddress() calculation doesn't match actual deployment. Solution: Use a different salt, or if account exists, remove initCode from UserOp and use existing account.")
		} else if isAccountExists {
			logEntry.
				Int("initCodeLen", len(userOp.InitCode)).
				Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
				Str("sender", userOp.Sender.Hex()).
				Str("innerRevertReason", innerRevertReason).
				Msg("AA13: account already exists - The account at the sender address already exists. Solution: Remove initCode from UserOp and use existing account, or use a different salt to create a new account.")
		} else {
			logEntry.
				Int("initCodeLen", len(userOp.InitCode)).
				Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
				Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
				Str("innerErrorSelector", innerErrorSelector).
				Str("innerRevertReason", innerRevertReason).
				Msg("AA13: initCode failed or OOG - Account creation failed or ran out of gas. Check: 1) factory address is correct, 2) factory.createAccount() function exists and is callable, 3) verificationGasLimit is sufficient for account creation, 4) factory contract has sufficient gas to deploy account, 5) account doesn't already exist at sender address")
		}
	
	case "AA20":
		logEntry.
			Msg("AA20: account not staked - Account needs to be staked to use paymaster. Check: 1) account has been staked via EntryPoint.depositTo(), 2) stake amount meets minimum requirements")
	
	case "AA21":
		logEntry.
			Str("senderBalance", "check on-chain").
			Str("requiredPrefund", "calculated by EntryPoint").
			Msg("AA21: didn't pay prefund - Account doesn't have enough balance to cover prefund (gas deposit). Check: 1) sender address has sufficient native token balance, 2) account was funded before submitting UserOp, 3) prefund amount = verificationGasLimit * maxFeePerGas")
	
	case "AA22":
		logEntry.
			Msg("AA22: returned prefund - Account returned prefund during validation. This is unusual and may indicate a bug in the account contract's validation logic")
	
	case "AA23":
		// AA23 can be caused by signature validation failure or execution failure
		// Prioritize checking for signature validation failure (0xf645eedf from SimpleAccount)
		// even if callData is present, as signature validation happens before execution
		isSignatureValidationFailure := innerErrorSelector == "0xf645eedf" || strings.Contains(strings.ToLower(revertReasonStr), "signature")
		
		// Check for proxy-related msg.sender issues
		isProxyIssue := strings.Contains(strings.ToLower(revertReasonStr), "notownerorentrypoint") ||
			strings.Contains(strings.ToLower(revertReasonStr), "notfromentrypoint") ||
			strings.Contains(strings.ToLower(revertReasonStr), "msg.sender") ||
			strings.Contains(strings.ToLower(innerRevertReason), "notownerorentrypoint") ||
			strings.Contains(strings.ToLower(innerRevertReason), "notfromentrypoint")
		
		if isSignatureValidationFailure || innerErrorSelector == "0xf645eedf" {
			// Signature validation failed (even if callData is present, the failure is in validation)
			logEntry.
				Str("signatureV", signatureV).
				Str("signatureR", signatureR).
				Str("signatureS", signatureS).
				Str("signatureHex", hexutil.Encode(userOp.Signature)).
				Int("signatureLen", len(userOp.Signature)).
				Str("expectedUserOpHash", userOpHash.Hex()).
				Str("innerErrorSelector", innerErrorSelector).
				Str("innerRevertReason", innerRevertReason).
				Str("entryPoint", e.entryPointAddr.Hex()).
				Str("chainID", e.evmChainID.String()).
				Msg("AA23: signature validation failed (inner error 0xf645eedf) - Account was created but signature validation failed. Check: 1) signature v value (should be 0 or 1 for SimpleAccount, not 27/28), 2) UserOp hash calculation matches frontend (expected hash logged above), 3) signature was signed over correct hash, 4) chainID matches (545 for flow-testnet, 747 for flow-mainnet, 646 for flow-previewnet/emulator), 5) signature (r, s) values are correct")
		} else if isProxyIssue {
			// Proxy-related msg.sender issue
			logEntry.
				Str("sender", userOp.Sender.Hex()).
				Str("entryPoint", e.entryPointAddr.Hex()).
				Str("innerErrorSelector", innerErrorSelector).
				Str("innerRevertReason", innerRevertReason).
				Str("revertReason", revertReasonStr).
				Msg("AA23: likely proxy msg.sender issue - Account uses proxy pattern but account implementation checks msg.sender == EntryPoint, which fails because msg.sender is the proxy address. This is NOT a gateway issue - the gateway works with any ERC-4337 account. Solution: 1) Use a production account factory (ZeroDev, Alchemy) that handles proxies correctly, 2) Deploy SimpleAccount directly without proxy, 3) Make account implementation proxy-aware. Gateway is account-agnostic and works with production accounts.")
		} else if len(userOp.CallData) == 0 {
			logEntry.
				Str("signatureV", signatureV).
				Str("signatureR", signatureR).
				Str("signatureS", signatureS).
				Str("signatureHex", hexutil.Encode(userOp.Signature)).
				Int("signatureLen", len(userOp.Signature)).
				Str("expectedUserOpHash", userOpHash.Hex()).
				Str("innerErrorSelector", innerErrorSelector).
				Str("innerRevertReason", innerRevertReason).
				Msg("AA23: reverted (empty callData) - Signature validation likely failed. Check: 1) signature v value (should be 0 or 1 for SimpleAccount, not 27/28), 2) UserOp hash calculation matches frontend (expected hash logged above), 3) signature was signed over correct hash, 4) chainID matches")
		} else {
			// Try to decode callData to see what function was being called
			functionSelector := ""
			if len(userOp.CallData) >= 4 {
				functionSelector = hexutil.Encode(userOp.CallData[:4])
			}
			logEntry.
				Str("callDataFunctionSelector", functionSelector).
				Str("callDataHex", hexutil.Encode(userOp.CallData)).
				Str("expectedUserOpHash", userOpHash.Hex()).
				Str("innerErrorSelector", innerErrorSelector).
				Str("innerRevertReason", innerRevertReason).
				Msg("AA23: reverted (non-empty callData) - Account execution failed. Check: 1) account function exists and is callable, 2) function parameters are correct, 3) account has sufficient balance for transfers, 4) account contract logic doesn't revert")
		}
	
	case "AA24":
		logEntry.
			Str("signatureV", signatureV).
			Str("signatureR", signatureR).
			Str("signatureS", signatureS).
			Str("signatureHex", hexutil.Encode(userOp.Signature)).
			Int("signatureLen", len(userOp.Signature)).
			Str("expectedUserOpHash", userOpHash.Hex()).
			Msg("AA24: signature error - Account was created but signature validation failed. Check: 1) signature v value (should be 0 or 1 for SimpleAccount, not 27/28), 2) UserOp hash calculation matches frontend (expected hash logged above), 3) signature was signed over correct hash, 4) chainID matches (545 for flow-testnet, 747 for flow-mainnet, 646 for flow-previewnet/emulator), 5) signature format is correct (65 bytes)")
	
	case "AA25":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Str("paymasterAndDataHex", hexutil.Encode(userOp.PaymasterAndData)).
			Msg("AA25: paymaster validation failed - Paymaster's validatePaymasterUserOp() reverted. Check: 1) paymaster contract is deployed and functional, 2) paymaster signature is correct (if using signature-based validation), 3) paymaster validation logic doesn't revert, 4) paymaster has sufficient deposit")
	
	case "AA26":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Msg("AA26: paymaster postOp failed - Paymaster's _postOp() reverted. Check: 1) paymaster contract's postOp logic doesn't revert, 2) paymaster has sufficient deposit for postOp operations, 3) postOp parameters are correct")
	
	case "AA30":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Msg("AA30: paymaster deposit too low - Paymaster deposit is insufficient. Check: 1) paymaster has sufficient deposit via EntryPoint.depositTo(), 2) deposit amount covers all gas costs")
	
	case "AA31":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Msg("AA31: paymaster stake too low - Paymaster stake is below minimum required. Check: 1) paymaster has been staked via EntryPoint.addStake(), 2) stake amount meets minimum requirements")
	
	case "AA32":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Msg("AA32: paymaster expired - Paymaster's validity period has expired. Check: 1) paymaster's validUntil timestamp is in the future, 2) current block timestamp is within validity period")
	
	case "AA33":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Msg("AA33: paymaster not staked - Paymaster needs to be staked. Check: 1) paymaster has been staked via EntryPoint.addStake(), 2) stake unlock delay has passed (if applicable)")
	
	case "AA40":
		logEntry.
			Msg("AA40: opcode validation failed - UserOp used a forbidden opcode during validation. Check: 1) account validation logic doesn't use forbidden opcodes, 2) account contract follows ERC-4337 validation rules")
	
	case "AA41":
		logEntry.
			Str("signatureV", signatureV).
			Str("signatureLen", fmt.Sprintf("%d", len(userOp.Signature))).
			Str("signatureHex", hexutil.Encode(userOp.Signature)).
			Msg("AA41: wrong signature format - Signature format is incorrect. Check: 1) signature is exactly 65 bytes, 2) signature v value is 0 or 1 (not 27/28), 3) signature r and s values are valid")
	
	case "AA42":
		logEntry.
			Str("userOpNonce", userOp.Nonce.String()).
			Msg("AA42: invalid nonce - UserOp nonce doesn't match account's expected nonce. Check: 1) nonce matches account's current nonce, 2) no other UserOps with same nonce were submitted, 3) nonce is sequential (for sequential nonce accounts)")
	
	case "AA43":
		logEntry.
			Str("userOpNonce", userOp.Nonce.String()).
			Msg("AA43: invalid account nonce - Account nonce validation failed. Check: 1) account's nonce validation logic, 2) nonce matches account's expected value")
	
	case "AA50":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Str("verificationGasLimit", userOp.VerificationGasLimit.String()).
			Msg("AA50: paymaster out of gas - Paymaster validation ran out of gas. Check: 1) verificationGasLimit is sufficient for paymaster validation, 2) paymaster validation logic is gas-efficient")
	
	case "AA51":
		logEntry.
			Str("paymasterAddress", paymasterAddress).
			Str("paymasterAndDataHex", hexutil.Encode(userOp.PaymasterAndData)).
			Msg("AA51: invalid paymaster signature - Paymaster signature validation failed. Check: 1) paymaster signature is correct, 2) signature was signed over correct data, 3) paymaster signature format matches expected format")
	
	default:
		// Unknown or no AA error code
		if aaErrorCode != "" {
			logEntry.
				Msg(fmt.Sprintf("AA error %s: %s - See revertReasonHex and innerRevertReason for details", aaErrorCode, revertReasonStr))
		} else {
			logEntry.
				Msg("UserOperation failed - no AA error code detected. See revertReasonHex and innerRevertReason for details")
		}
	}
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
