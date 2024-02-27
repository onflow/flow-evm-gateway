package ingestion

import (
	"bytes"
	"context"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/ingestion/mocks"
	storageMock "github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

func TestSerialBlockIngestion(t *testing.T) {
	t.Run("successfully ingest serial blocks", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestHeight").
			Return(func() (uint64, error) {
				return latestHeight, nil
			}).
			Once() // make sure this isn't called multiple times

		eventsChan := make(chan flow.BlockEvents)
		subscriber := &mocks.Subscriber{}
		subscriber.
			On("Subscribe", mock.AnythingOfType("context.backgroundCtx"), mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
				return eventsChan, make(<-chan error), nil
			})

		engine := NewEventIngestionEngine(subscriber, blocks, receipts, transactions, zerolog.Nop())

		go func() {
			err := engine.Start(context.Background())
			require.NoError(t, err)
		}()

		for i := latestHeight + 1; i < latestHeight+20; i++ {
			blockCdc, block, blockEvent, err := newBlock(i)
			require.NoError(t, err)

			blocks.
				On("Store", mock.AnythingOfType("*types.Block")).
				Return(func(storeBlock *types.Block) error {
					assert.Equal(t, block, storeBlock)
					return nil
				}).
				Once()

			eventsChan <- flow.BlockEvents{
				Events: []flow.Event{{
					Type:  string(blockEvent.Etype),
					Value: blockCdc,
				}},
			}
		}

		close(eventsChan)
		// todo <-engine.Done()
	})

	t.Run("fail with events out of sequence", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)

		blocks := &storageMock.BlockIndexer{}
		blocks.
			On("LatestHeight").
			Return(func() (uint64, error) {
				return latestHeight, nil
			}).
			Once() // make sure this isn't called multiple times

		eventsChan := make(chan flow.BlockEvents)
		subscriber := &mocks.Subscriber{}
		subscriber.
			On("Subscribe", mock.AnythingOfType("context.backgroundCtx"), mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
				return eventsChan, make(<-chan error), nil
			})

		engine := NewEventIngestionEngine(subscriber, blocks, receipts, transactions, zerolog.Nop())

		waitErr := make(chan struct{})
		// catch eventual error due to out of sequence block height
		go func() {
			err := engine.Start(context.Background())
			assert.ErrorIs(t, err, models.InvalidHeightErr)
			assert.EqualError(t, err, "invalid block height, expected 11, got 20: invalid height")
			waitErr <- struct{}{}
		}()

		// first create one successful block event
		blockCdc, block, blockEvent, err := newBlock(latestHeight + 1)
		require.NoError(t, err)

		blocks.
			On("Store", mock.AnythingOfType("*types.Block")).
			Return(func(storeBlock *types.Block) error {
				assert.Equal(t, block, storeBlock)
				return nil
			}).
			Once() // this should only be called for first valid block

		eventsChan <- flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}},
		}

		// fail with next block height being incorrect
		blockCdc, _, blockEvent, err = newBlock(latestHeight + 10) // not sequential next block height
		require.NoError(t, err)

		eventsChan <- flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}},
		}

		close(eventsChan)
		<-waitErr
	})

}

func TestTransactionIngestion(t *testing.T) {
	receipts := &storageMock.ReceiptIndexer{}
	transactions := &storageMock.TransactionIndexer{}
	latestHeight := uint64(10)

	blocks := &storageMock.BlockIndexer{}
	blocks.
		On("LatestHeight").
		Return(func() (uint64, error) {
			return latestHeight, nil
		}).
		Once() // make sure this isn't called multiple times

	eventsChan := make(chan flow.BlockEvents)
	subscriber := &mocks.Subscriber{}
	subscriber.
		On("Subscribe", mock.AnythingOfType("context.backgroundCtx"), mock.AnythingOfType("uint64")).
		Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
			return eventsChan, make(<-chan error), nil
		})

	engine := NewEventIngestionEngine(subscriber, blocks, receipts, transactions, zerolog.Nop())

	go func() {
		err := engine.Start(context.Background())
		require.NoError(t, err)
	}()

	txCdc, event, transaction, result, err := newTransaction()
	require.NoError(t, err)

	transactions.
		On("Store", mock.AnythingOfType("*gethTypes.Transaction")).
		Return(func(tx *gethTypes.Transaction) error {
			assert.Equal(t, transaction, tx)
			return nil
		}).
		Once()

	receipts.
		On("Store", mock.AnythingOfType("*gethTypes.Receipt")).
		Return(func(rcp *gethTypes.Receipt) error {
			assert.EqualValues(t, result.Logs, rcp.Logs)
			assert.Equal(t, result.DeployedContractAddress, rcp.ContractAddress)
			return nil
		}).
		Once()

	eventsChan <- flow.BlockEvents{
		Events: []flow.Event{{
			Type:  string(event.Etype),
			Value: txCdc,
		}},
	}

	close(eventsChan)
	// todo <-engine.Done()
}

func newBlock(height uint64) (cadence.Event, *types.Block, *types.Event, error) {
	block := &types.Block{
		ParentBlockHash: common.HexToHash("0x1"),
		Height:          height,
		TotalSupply:     big.NewInt(100),
		ReceiptRoot:     common.HexToHash("0x2"),
		TransactionHashes: []common.Hash{
			common.HexToHash("0xf1"),
		},
	}

	blockEvent := types.NewBlockExecutedEvent(block)
	blockCdc, err := blockEvent.Payload.CadenceEvent()

	return blockCdc, block, blockEvent, err
}

func newTransaction() (cadence.Event, *types.Event, *gethTypes.Transaction, *types.Result, error) {
	res := &types.Result{
		Failed:                  false,
		TxType:                  1,
		GasConsumed:             1337,
		DeployedContractAddress: types.Address{0x5, 0x6, 0x7},
		ReturnedValue:           []byte{0x55},
		Logs: []*gethTypes.Log{{
			Address: common.Address{0x1, 0x2},
			Topics:  []common.Hash{{0x5, 0x6}, {0x7, 0x8}},
		}, {
			Address: common.Address{0x3, 0x5},
			Topics:  []common.Hash{{0x2, 0x66}, {0x7, 0x1}},
		}},
	}

	tx := gethTypes.NewTx(&gethTypes.AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    1,
		GasPrice: big.NewInt(24),
		Gas:      1337,
		To:       &common.Address{0x01},
		Value:    big.NewInt(5),
		Data:     []byte{0x2, 0x3},
	})

	var txEnc bytes.Buffer
	err := tx.EncodeRLP(&txEnc)
	if err != nil {
		return cadence.Event{}, nil, nil, nil, err
	}

	ev := types.NewTransactionExecutedEvent(1, txEnc.Bytes(), tx.Hash(), res)

	cdcEv, err := ev.Payload.CadenceEvent()

	return cdcEv, ev, tx, res, err
}
