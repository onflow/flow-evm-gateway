package ingestion

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/ingestion/mocks"
	storageMock "github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/onflow/flow-go-sdk"
	broadcast "github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSerialBlockIngestion(t *testing.T) {
	t.Run("successfully ingest serial blocks", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)

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

		eventsChan := make(chan flow.BlockEvents)
		subscriber := &mocks.Subscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
				return eventsChan, make(<-chan error), nil
			})

		engine := NewEventIngestionEngine(
			subscriber,
			blocks,
			receipts,
			transactions,
			accounts,
			broadcast.NewBroadcaster(),
			zerolog.Nop(),
		)

		done := make(chan struct{})
		go func() {
			err := engine.Run(context.Background())
			assert.ErrorIs(t, err, models.ErrDisconnected) // we disconnect at the end
			close(done)
		}()

		storedCounter := 0
		runs := uint64(20)
		for i := latestHeight + 1; i < latestHeight+runs; i++ {
			cadenceHeight := i + 10
			blockCdc, block, blockEvent, err := newBlock(i)
			require.NoError(t, err)

			blocks.
				On("Store", mock.AnythingOfType("uint64"), mock.AnythingOfType("*types.Block")).
				Return(func(h uint64, storeBlock *types.Block) error {
					assert.Equal(t, block, storeBlock)
					assert.Equal(t, cadenceHeight, h)
					storedCounter++
					return nil
				}).
				Once()

			eventsChan <- flow.BlockEvents{
				Events: []flow.Event{{
					Type:  string(blockEvent.Etype),
					Value: blockCdc,
				}},
				Height: cadenceHeight,
			}
		}

		close(eventsChan)
		<-done
		assert.Equal(t, runs-1, uint64(storedCounter))
		// todo <-engine.Done()
	})

	t.Run("fail with events out of sequence", func(t *testing.T) {
		receipts := &storageMock.ReceiptIndexer{}
		transactions := &storageMock.TransactionIndexer{}
		latestHeight := uint64(10)

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

		eventsChan := make(chan flow.BlockEvents)
		subscriber := &mocks.Subscriber{}
		subscriber.
			On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
			Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
				return eventsChan, make(<-chan error), nil
			})

		engine := NewEventIngestionEngine(
			subscriber,
			blocks,
			receipts,
			transactions,
			accounts,
			broadcast.NewBroadcaster(),
			zerolog.Nop(),
		)

		waitErr := make(chan struct{})
		// catch eventual error due to out of sequence block height
		go func() {
			err := engine.Run(context.Background())
			assert.ErrorIs(t, err, models.ErrInvalidHeight)
			assert.EqualError(t, err, "failed to process event: invalid block height, expected 11, got 20: invalid height")
			close(waitErr)
		}()

		// first create one successful block event
		blockCdc, block, blockEvent, err := newBlock(latestHeight + 1)
		cadenceHeight := latestHeight + 10
		require.NoError(t, err)

		blocks.
			On("Store", mock.AnythingOfType("uint64"), mock.AnythingOfType("*types.Block")).
			Return(func(h uint64, storeBlock *types.Block) error {
				assert.Equal(t, block, storeBlock)
				assert.Equal(t, cadenceHeight, h)
				return nil
			}).
			Once() // this should only be called for first valid block

		eventsChan <- flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}},
			Height: cadenceHeight,
		}

		// fail with next block height being incorrect
		blockCdc, _, blockEvent, err = newBlock(latestHeight + 10) // not sequential next block height
		require.NoError(t, err)

		eventsChan <- flow.BlockEvents{
			Events: []flow.Event{{
				Type:  string(blockEvent.Etype),
				Value: blockCdc,
			}},
			Height: cadenceHeight + 1,
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
		On("LatestCadenceHeight").
		Return(func() (uint64, error) {
			return latestHeight, nil
		}).
		Once() // make sure this isn't called multiple times

	blocks.
		On("SetLatestCadenceHeight", mock.AnythingOfType("uint64")).
		Return(func(h uint64) error {
			assert.Equal(t, latestHeight+1, h)
			return nil
		})

	accounts := &storageMock.AccountIndexer{}
	accounts.
		On("Update", mock.AnythingOfType("*types.Transaction"), mock.AnythingOfType("*types.Receipt")).
		Return(func(tx *gethTypes.Transaction, receipt *gethTypes.Receipt) error { return nil })

	eventsChan := make(chan flow.BlockEvents)
	subscriber := &mocks.Subscriber{}
	subscriber.
		On("Subscribe", mock.Anything, mock.AnythingOfType("uint64")).
		Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
			return eventsChan, make(<-chan error), nil
		})

	engine := NewEventIngestionEngine(
		subscriber,
		blocks,
		receipts,
		transactions,
		accounts,
		broadcast.NewBroadcaster(),
		zerolog.Nop(),
	)

	done := make(chan struct{})
	go func() {
		err := engine.Run(context.Background())
		assert.ErrorIs(t, err, models.ErrDisconnected) // we disconnect at the end
		close(done)
	}()

	txCdc, event, transaction, result, err := newTransaction()
	require.NoError(t, err)

	transactions.
		On("Store", mock.AnythingOfType("*types.Transaction")).
		Return(func(tx *gethTypes.Transaction) error {
			assert.Equal(t, transaction.Hash(), tx.Hash()) // if hashes are equal tx is equal
			return nil
		}).
		Once()

	receipts.
		On("Store", mock.AnythingOfType("*types.Receipt")).
		Return(func(rcp *gethTypes.Receipt) error {
			assert.Len(t, rcp.Logs, len(result.Logs))
			assert.Equal(t, result.DeployedContractAddress.ToCommon().String(), rcp.ContractAddress.String())
			return nil
		}).
		Once()

	eventsChan <- flow.BlockEvents{
		Events: []flow.Event{{
			Type:  string(event.Etype),
			Value: txCdc,
		}},
		Height: latestHeight + 1,
	}

	close(eventsChan)
	<-done
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
		VMError:                 nil,
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

	ev := types.NewTransactionExecutedEvent(
		1,
		txEnc.Bytes(),
		common.HexToHash("0x1"),
		tx.Hash(),
		res,
	)

	cdcEv, err := ev.Payload.CadenceEvent()

	return cdcEv, ev, tx, res, err
}
