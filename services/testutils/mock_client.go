package testutils

import (
	"context"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/access/mocks"
	"github.com/onflow/flow-go/storage"
)

type MockClient struct {
	*mocks.Client
	GetLatestBlockHeaderFunc         func(context.Context, bool) (*flow.BlockHeader, error)
	GetBlockHeaderByHeightFunc       func(context.Context, uint64) (*flow.BlockHeader, error)
	SubscribeEventsByBlockHeightFunc func(context.Context, uint64, flow.EventFilter, ...access.SubscribeOption) (<-chan flow.BlockEvents, <-chan error, error)
	GetNodeVersionInfoFunc           func(ctx context.Context) (*flow.NodeVersionInfo, error)
	GetEventsForHeightRangeFunc      func(
		ctx context.Context, eventType string, startHeight uint64, endHeight uint64,
	) ([]flow.BlockEvents, error)
	GetTransactionResultsByBlockIDFunc func(ctx context.Context, blockID flow.Identifier) ([]*flow.TransactionResult, error)
}

func (c *MockClient) GetBlockHeaderByHeight(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
	return c.GetBlockHeaderByHeightFunc(ctx, height)
}

func (c *MockClient) GetLatestBlockHeader(ctx context.Context, sealed bool) (*flow.BlockHeader, error) {
	return c.GetLatestBlockHeaderFunc(ctx, sealed)
}

func (c *MockClient) GetNodeVersionInfo(ctx context.Context) (*flow.NodeVersionInfo, error) {
	return c.GetNodeVersionInfoFunc(ctx)
}

func (c *MockClient) SubscribeEventsByBlockHeight(
	ctx context.Context,
	startHeight uint64,
	filter flow.EventFilter,
	opts ...access.SubscribeOption,
) (<-chan flow.BlockEvents, <-chan error, error) {
	return c.SubscribeEventsByBlockHeightFunc(ctx, startHeight, filter, opts...)
}

func (c *MockClient) GetEventsForHeightRange(
	ctx context.Context, eventType string, startHeight uint64, endHeight uint64,
) ([]flow.BlockEvents, error) {
	if c.GetEventsForHeightRangeFunc != nil {
		return c.GetEventsForHeightRangeFunc(ctx, eventType, startHeight, endHeight)
	}
	return c.Client.GetEventsForHeightRange(ctx, eventType, startHeight, endHeight)
}

func (c *MockClient) GetTransactionResultsByBlockID(
	ctx context.Context,
	blockID flow.Identifier,
) ([]*flow.TransactionResult, error) {
	if c.GetTransactionResultsByBlockIDFunc != nil {
		return c.GetTransactionResultsByBlockIDFunc(ctx, blockID)
	}
	return c.Client.GetTransactionResultsByBlockID(ctx, blockID)
}

func SetupClientForRange(startHeight uint64, endHeight uint64) *MockClient {
	client, events := SetupClient(startHeight, endHeight)
	go func() {
		defer close(events)

		for i := startHeight; i <= endHeight; i++ {
			events <- flow.BlockEvents{
				Height: i,
			}
		}
	}()

	return client
}

func SetupClient(startHeight uint64, endHeight uint64) (*MockClient, chan flow.BlockEvents) {
	events := make(chan flow.BlockEvents)

	return &MockClient{
		Client: &mocks.Client{},
		GetLatestBlockHeaderFunc: func(ctx context.Context, sealed bool) (*flow.BlockHeader, error) {
			return &flow.BlockHeader{
				Height: endHeight,
			}, nil
		},
		GetBlockHeaderByHeightFunc: func(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
			if height < startHeight || height > endHeight {
				return nil, storage.ErrNotFound
			}

			return &flow.BlockHeader{
				Height: height,
			}, nil
		},
		GetNodeVersionInfoFunc: func(ctx context.Context) (*flow.NodeVersionInfo, error) {
			return &flow.NodeVersionInfo{
				NodeRootBlockHeight:  startHeight,
				SporkRootBlockHeight: startHeight,
			}, nil
		},
		SubscribeEventsByBlockHeightFunc: func(
			ctx context.Context,
			startHeight uint64,
			filter flow.EventFilter,
			opts ...access.SubscribeOption,
		) (<-chan flow.BlockEvents, <-chan error, error) {
			return events, make(chan error), nil
		},
		GetEventsForHeightRangeFunc: func(
			ctx context.Context, eventType string, sh uint64, eh uint64,
		) ([]flow.BlockEvents, error) {
			if sh < startHeight || sh > endHeight {
				return nil, storage.ErrNotFound
			}
			if eh < startHeight || eh > endHeight {
				return nil, storage.ErrNotFound
			}

			evts := make([]flow.BlockEvents, 0, eh-sh+1)
			for i := uint64(0); i <= eh-sh; i++ {
				evts = append(evts, flow.BlockEvents{
					Height: sh + i,
				})
			}

			return evts, nil
		},
	}, events
}
