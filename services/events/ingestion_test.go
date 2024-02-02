package events

import (
	"context"
	"github.com/onflow/flow-evm-gateway/services/events/mocks"
	storageMock "github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBlockIngestion(t *testing.T) {
	receipts := &storageMock.ReceiptIndexer{}
	transactions := &storageMock.TransactionIndexer{}

	latestHeight := uint64(10)

	blocks := &storageMock.BlockIndexer{}
	blocks.On("LatestHeight").Return(func() (uint64, error) {
		return latestHeight, nil
	})
	blocks.On("Store").Return(func(block *types.Block) error {
		// todo assert correct block
		return nil
	})

	eventsChan := make(<-chan flow.BlockEvents)
	subscriber := &mocks.Subscriber{}
	subscriber.
		On("Subscribe").
		Return(func(ctx context.Context, latest uint64) (<-chan flow.BlockEvents, <-chan error, error) {
			return eventsChan, make(<-chan error), nil
		})

	engine := NewEventIngestionEngine(subscriber, blocks, receipts, transactions, zerolog.Nop())

	err := engine.Start(context.Background())
	require.NoError(t, err)

}
