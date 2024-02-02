package events

import (
	"context"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/services/events/mocks"
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

func newBlock(height uint64) (*types.Block, cadence.Event, *types.Event, error) {
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

	return block, blockCdc, blockEvent, err
}

func TestSerialBlockIngestion(t *testing.T) {
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
		block, blockCdc, blockEvent, err := newBlock(i)
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
}
