package ingestion

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"

	pebbleDB "github.com/cockroachdb/pebble"
	"github.com/onflow/flow-go/fvm/evm/events"
	flowGo "github.com/onflow/flow-go/model/flow"

	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/services/ingestion/mocks"
	"github.com/onflow/flow-evm-gateway/storage/pebble"

	"github.com/onflow/cadence"

	"github.com/onflow/flow-evm-gateway/models"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	storageMock "github.com/onflow/flow-evm-gateway/storage/mocks"
)

func TestSerialBlockIngestion(t *testing.T) {
	t.Run("successfully ingest serial blocks", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)

		store, err := pebble.New(t.TempDir(), zerolog.Nop())
		require.NoError(t, err)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestCadenceHeight").
			Return(func() (uint64, error) {
				return latestHeight, nil
			}).
			Once() // make sure this isn't called multiple times

		accounts := &storageMock.AccountIndexer{}
		accounts.
			On("Update").
			Return(func() error { return nil })

		eventsChan := make(chan models.BlockEvents)

		subscriber := &mocks.EventSubscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) <-chan models.BlockEvents {
				return eventsChan
			})

		engine := NewEventIngestionEngine(
			subscriber,
			store,
			blocks,
			receipts,
			transactions,
			accounts,
			models.NewPublisher(),
			models.NewPublisher(),
			zerolog.Nop(),
			metrics.NopCollector,
		)

		done := make(chan struct{})
		go func() {
			err := engine.Run(context.Background())
			assert.NoError(t, err)
			close(done)
		}()

		storedCounter := 0
		runs := uint64(20)
		for i := latestHeight + 1; i < latestHeight+runs; i++ {
			cadenceHeight := i + 10
			blockCdc, block, blockEvent, err := newBlock(i, nil)
			require.NoError(t, err)

			blocks.
				On("Store", mock.AnythingOfType("uint64"), mock.Anything, mock.AnythingOfType("*models.Block"), mock.Anything).
				Return(func(h uint64, id flow.Identifier, storeBlock *models.Block, _ *pebbleDB.Batch) error {
					assert.Equal(t, block, storeBlock)
					assert.Equal(t, cadenceHeight, h)
					storedCounter++
					return nil
				}).
				Once()

			eventsChan <- models.NewBlockEvents(flow.BlockEvents{
				Events: []flow.Event{{
					Type:  string(blockEvent.Etype),
					Value: blockCdc,
				}},
				Height: cadenceHeight,
			})
		}

		close(eventsChan)
		<-done
		assert.Equal(t, runs-1, uint64(storedCounter))
	})

	t.Run("fail with events out of sequence", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)

		store, err := pebble.New(t.TempDir(), zerolog.Nop())
		require.NoError(t, err)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestCadenceHeight").
			Return(func() (uint64, error) {
				return latestHeight, nil
			}).
			Once() // make sure this isn't called multiple times

		accounts := &storageMock.AccountIndexer{}
		accounts.
			On("Update", mock.Anything, mock.Anything).
			Return(func(t models.TransactionCall, r *gethTypes.Receipt) error { return nil })

		eventsChan := make(chan models.BlockEvents)
		subscriber := &mocks.EventSubscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) <-chan models.BlockEvents {
				return eventsChan
			})

		engine := NewEventIngestionEngine(
			subscriber,
			store,
			blocks,
			receipts,
			transactions,
			accounts,
			models.NewPublisher(),
			models.NewPublisher(),
			zerolog.Nop(),
			metrics.NopCollector,
		)

		waitErr := make(chan struct{})
		// catch eventual error due to out of sequence block height
		go func() {
			err := engine.Run(context.Background())
			assert.ErrorIs(t, err, models.ErrInvalidHeight)
			assert.EqualError(t, err, "failed to index block 20 event: invalid block height, expected 11, got 20: invalid height")
			close(waitErr)
		}()

		// first create one successful block event
		blockCdc, block, blockEvent, err := newBlock(latestHeight+1, nil)
		cadenceHeight := latestHeight + 10
		require.NoError(t, err)

		blocks.
			On("Store", mock.AnythingOfType("uint64"), mock.Anything, mock.AnythingOfType("*models.Block"), mock.Anything).
			Return(func(h uint64, id flow.Identifier, storeBlock *models.Block, _ *pebbleDB.Batch) error {
				assert.Equal(t, block, storeBlock)
				assert.Equal(t, cadenceHeight, h)
				return nil
			}).
			Once() // this should only be called for first valid block

		cadenceEvents, err := models.NewCadenceEvents(flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}},
			Height: cadenceHeight,
		})
		require.NoError(t, err)

		eventsChan <- models.BlockEvents{
			Events: cadenceEvents,
		}

		// fail with next block height being incorrect
		blockCdc, _, blockEvent, err = newBlock(latestHeight+10, nil) // not sequential next block height
		require.NoError(t, err)

		cadenceEvents, err = models.NewCadenceEvents(flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}},
			Height: cadenceHeight + 1,
		})
		require.NoError(t, err)

		eventsChan <- models.BlockEvents{
			Events: cadenceEvents,
		}

		close(eventsChan)
		<-waitErr
	})

}

func TestBlockAndTransactionIngestion(t *testing.T) {

	t.Run("successfully ingest transaction and block", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)
		nextHeight := latestHeight + 1
		blockID := flow.Identifier{0x01}

		store, err := pebble.New(t.TempDir(), zerolog.Nop())
		require.NoError(t, err)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestCadenceHeight").
			Return(func() (uint64, error) {
				return latestHeight, nil
			}).
			Once() // make sure this isn't called multiple times

		blocks.
			On("SetLatestCadenceHeight", mock.AnythingOfType("uint64"), mock.Anything).
			Return(func(h uint64, _ *pebbleDB.Batch) error {
				assert.Equal(t, nextHeight, h)
				return nil
			})

		accounts := &storageMock.AccountIndexer{}
		accounts.
			On("Update", mock.AnythingOfType("models.TransactionCall"), mock.AnythingOfType("*models.Receipt"), mock.Anything).
			Return(func(tx models.Transaction, receipt *models.Receipt, _ *pebbleDB.Batch) error { return nil })

		eventsChan := make(chan models.BlockEvents)
		subscriber := &mocks.EventSubscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) <-chan models.BlockEvents {
				return eventsChan
			})

		txCdc, txEvent, transaction, result, err := newTransaction(nextHeight)
		require.NoError(t, err)
		blockCdc, block, blockEvent, err := newBlock(nextHeight, []gethCommon.Hash{result.TxHash})
		require.NoError(t, err)

		engine := NewEventIngestionEngine(
			subscriber,
			store,
			blocks,
			receipts,
			transactions,
			accounts,
			models.NewPublisher(),
			models.NewPublisher(),
			zerolog.Nop(),
			metrics.NopCollector,
		)

		done := make(chan struct{})
		go func() {
			err := engine.Run(context.Background())
			assert.NoError(t, err)
			close(done)
		}()

		blocks.
			On("Store", mock.AnythingOfType("uint64"), mock.Anything, mock.AnythingOfType("*models.Block"), mock.Anything).
			Return(func(h uint64, id flow.Identifier, storeBlock *models.Block, _ *pebbleDB.Batch) error {
				assert.Equal(t, block, storeBlock)
				assert.Equal(t, blockID, id)
				assert.Equal(t, nextHeight, h)
				return nil
			}).
			Once()

		transactions.
			On("Store", mock.AnythingOfType("models.TransactionCall"), mock.Anything).
			Return(func(tx models.Transaction, _ *pebbleDB.Batch) error {
				assert.Equal(t, transaction.Hash(), tx.Hash()) // if hashes are equal tx is equal
				return nil
			}).
			Once()

		receipts.
			On("Store", mock.AnythingOfType("[]*models.Receipt"), mock.Anything).
			Return(func(receipts []*models.Receipt, _ *pebbleDB.Batch) error {
				assert.Len(t, receipts, 1)
				rcp := receipts[0]

				assert.Equal(t, nextHeight, rcp.BlockNumber.Uint64())
				assert.Len(t, rcp.Logs, len(result.Logs))
				assert.Equal(t, result.DeployedContractAddress.ToCommon().String(), rcp.ContractAddress.String())
				return nil
			}).
			Once()

		eventsChan <- models.NewBlockEvents(flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}, {
				Type:  string(txEvent.Etype),
				Value: txCdc,
			}},
			Height:  nextHeight,
			BlockID: blockID,
		})

		close(eventsChan)
		<-done
	})

	t.Run("ingest block first and then transaction even if received out-of-order", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)
		nextHeight := latestHeight + 1

		store, err := pebble.New(t.TempDir(), zerolog.Nop())
		require.NoError(t, err)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestCadenceHeight").
			Return(func() (uint64, error) {
				return latestHeight, nil
			}).
			On("SetLatestCadenceHeight", mock.AnythingOfType("uint64")).
			Return(func(h uint64) error { return nil })

		accounts := &storageMock.AccountIndexer{}
		accounts.
			On("Update", mock.AnythingOfType("models.TransactionCall"), mock.AnythingOfType("*models.Receipt"), mock.Anything).
			Return(func(tx models.Transaction, receipt *models.Receipt, _ *pebbleDB.Batch) error { return nil })

		eventsChan := make(chan models.BlockEvents)
		subscriber := &mocks.EventSubscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) <-chan models.BlockEvents {
				return eventsChan
			})

		txCdc, txEvent, _, res, err := newTransaction(nextHeight)
		require.NoError(t, err)
		blockCdc, _, blockEvent, err := newBlock(nextHeight, []gethCommon.Hash{res.TxHash})
		require.NoError(t, err)

		engine := NewEventIngestionEngine(
			subscriber,
			store,
			blocks,
			receipts,
			transactions,
			accounts,
			models.NewPublisher(),
			models.NewPublisher(),
			zerolog.Nop(),
			metrics.NopCollector,
		)

		done := make(chan struct{})
		go func() {
			err := engine.Run(context.Background())
			assert.NoError(t, err)
			close(done)
		}()

		blocksFirst := false // flag indicating we stored block first
		blocks.
			On("Store", mock.AnythingOfType("uint64"), mock.Anything, mock.AnythingOfType("*models.Block"), mock.Anything).
			Return(func(h uint64, id flow.Identifier, storeBlock *models.Block, _ *pebbleDB.Batch) error {
				blocksFirst = true
				return nil
			}).
			Once()

		transactions.
			On("Store", mock.AnythingOfType("models.TransactionCall"), mock.Anything).
			Return(func(tx models.Transaction, _ *pebbleDB.Batch) error {
				require.True(t, blocksFirst)
				return nil
			}).
			Once()

		receipts.
			On("Store", mock.AnythingOfType("[]*models.Receipt"), mock.Anything).
			Return(func(receipts []*models.Receipt, _ *pebbleDB.Batch) error {
				require.True(t, blocksFirst)
				return nil
			}).
			Once()

		eventsChan <- models.NewBlockEvents(flow.BlockEvents{
			Events: []flow.Event{
				// first transaction
				{
					Type:  string(txEvent.Etype),
					Value: txCdc,
				},
				// and then block (out-of-order)
				{
					Type:  string(blockEvent.Etype),
					Value: blockCdc,
				}},
			Height: nextHeight,
		})

		close(eventsChan)
		<-done
	})

	t.Run("ingest block and multiple transactions in same block event, even if out-of-order", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestCadenceHeight := uint64(0)

		store, err := pebble.New(t.TempDir(), zerolog.Nop())
		require.NoError(t, err)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestCadenceHeight").
			Return(func() (uint64, error) {
				return latestCadenceHeight, nil
			}).
			Once() // make sure this isn't called multiple times

		accounts := &storageMock.AccountIndexer{}
		accounts.
			On("Update", mock.Anything, mock.AnythingOfType("*models.Receipt"), mock.Anything).
			Return(func(t models.Transaction, r *models.Receipt, _ *pebbleDB.Batch) error { return nil })

		eventsChan := make(chan models.BlockEvents)
		subscriber := &mocks.EventSubscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) <-chan models.BlockEvents {
				assert.Equal(t, latestCadenceHeight, latest)
				return eventsChan
			}).
			Once()

		engine := NewEventIngestionEngine(
			subscriber,
			store,
			blocks,
			receipts,
			transactions,
			accounts,
			models.NewPublisher(),
			models.NewPublisher(),
			zerolog.Nop(),
			metrics.NopCollector,
		)

		done := make(chan struct{})
		go func() {
			err := engine.Run(context.Background())
			assert.NoError(t, err)
			close(done)
		}()

		evmHeight := uint64(0)
		events := make([]flow.Event, 0)
		blockIndexedFirst := false
		txsStored := 0
		eventCount := 5
		txHashes := make([]gethCommon.Hash, eventCount)

		for i := 0; i < eventCount; i++ {
			txCdc, txEvent, transaction, res, err := newTransaction(evmHeight)
			txHashes[i] = res.TxHash
			require.NoError(t, err)

			// add a single transaction for each block
			transactions.
				On("Store", mock.AnythingOfType("models.TransactionCall"), mock.Anything).
				Return(func(tx models.Transaction, _ *pebbleDB.Batch) error {
					assert.Equal(t, transaction.Hash(), tx.Hash()) // if hashes are equal tx is equal
					require.True(t, blockIndexedFirst)
					txsStored++
					return nil
				}).
				Once()

			receipts.
				On("Store", mock.AnythingOfType("[]*models.Receipt"), mock.Anything).
				Return(func(receipts []*models.Receipt, _ *pebbleDB.Batch) error { return nil }).
				Once()

			events = append(events, flow.Event{
				Type:  string(txEvent.Etype),
				Value: txCdc,
			})
		}

		blocksStored := 0
		blockCdc, block, blockEvent, err := newBlock(evmHeight, txHashes)
		require.NoError(t, err)

		blocks.
			On("Store", mock.AnythingOfType("uint64"), mock.Anything, mock.AnythingOfType("*models.Block"), mock.Anything).
			Return(func(h uint64, id flow.Identifier, storeBlock *models.Block, _ *pebbleDB.Batch) error {
				assert.Equal(t, block, storeBlock)
				assert.Equal(t, evmHeight, block.Height)
				assert.Equal(t, latestCadenceHeight+1, h)
				blockIndexedFirst = true
				blocksStored++
				return nil
			}).
			Once()

		events = append(events, flow.Event{
			Type:  string(blockEvent.Etype),
			Value: blockCdc,
		})

		// this messes up order of events to test if we still process events in-order
		// it will make transaction event first and then block event
		events[0], events[1] = events[1], events[0]
		// and it will make the first block be swapped with second block out-of-order
		events[1], events[2] = events[2], events[1]

		eventsChan <- models.NewBlockEvents(flow.BlockEvents{
			Events: events,
			Height: latestCadenceHeight + 1,
		})

		close(eventsChan)
		<-done
		assert.Equal(t, eventCount, txsStored)
		assert.Equal(t, 1, blocksStored)
	})
}

func newBlock(height uint64, txHashes []gethCommon.Hash) (cadence.Event, *models.Block, *events.Event, error) {
	gethBlock := &types.Block{
		ParentBlockHash: gethCommon.HexToHash("0x1"),
		Height:          height,
		TotalSupply:     big.NewInt(100),
		ReceiptRoot:     gethCommon.HexToHash("0x2"),
	}
	block := &models.Block{
		Block:             gethBlock,
		TransactionHashes: txHashes,
	}

	blockEvent := events.NewBlockEvent(gethBlock)
	blockCdc, err := blockEvent.Payload.ToCadence(flowGo.Previewnet)
	return blockCdc, block, blockEvent, err
}

func newTransaction(height uint64) (cadence.Event, *events.Event, models.Transaction, *types.Result, error) {
	res := &types.Result{
		VMError:                 nil,
		TxType:                  1,
		GasConsumed:             1337,
		DeployedContractAddress: &types.Address{0x5, 0x6, 0x7},
		ReturnedData:            []byte{0x55},
		Logs: []*gethTypes.Log{{
			Address: gethCommon.Address{0x1, 0x2},
			Topics:  []gethCommon.Hash{{0x5, 0x6}, {0x7, 0x8}},
		}, {
			Address: gethCommon.Address{0x3, 0x5},
			Topics:  []gethCommon.Hash{{0x2, 0x66}, {0x7, 0x1}},
		}},
		TxHash: gethCommon.HexToHash("0x33"),
	}

	txEncoded, err := hex.DecodeString("f9015880808301e8488080b901086060604052341561000f57600080fd5b60eb8061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063c6888fa1146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60007f24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da600783026040518082815260200191505060405180910390a16007820290509190505600a165627a7a7230582040383f19d9f65246752244189b02f56e8d0980ed44e7a56c0b200458caad20bb002982052fa09c05a7389284dc02b356ec7dee8a023c5efd3a9d844fa3c481882684b0640866a057e96d0a71a857ed509bb2b7333e78b2408574b8cc7f51238f25c58812662653")
	if err != nil {
		return cadence.Event{}, nil, nil, nil, err
	}

	tx := &gethTypes.Transaction{}
	err = tx.UnmarshalBinary(txEncoded)
	if err != nil {
		return cadence.Event{}, nil, nil, nil, err
	}

	ev := events.NewTransactionEvent(
		res,
		txEncoded,
		height,
	)

	cdcEv, err := ev.Payload.ToCadence(flowGo.Previewnet)
	return cdcEv, ev, models.TransactionCall{Transaction: tx}, res, err
}
