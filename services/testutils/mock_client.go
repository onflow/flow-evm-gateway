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
	getLatestBlockHeaderFunc         func(context.Context, bool) (*flow.BlockHeader, error)
	getBlockHeaderByHeightFunc       func(context.Context, uint64) (*flow.BlockHeader, error)
	subscribeEventsByBlockHeightFunc func(context.Context, uint64, flow.EventFilter, ...access.SubscribeOption) (<-chan flow.BlockEvents, <-chan error, error)
	getNodeVersionInfoFunc           func(ctx context.Context) (*flow.NodeVersionInfo, error)
}

func (c *MockClient) GetBlockHeaderByHeight(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
	return c.getBlockHeaderByHeightFunc(ctx, height)
}

func (c *MockClient) GetLatestBlockHeader(ctx context.Context, sealed bool) (*flow.BlockHeader, error) {
	return c.getLatestBlockHeaderFunc(ctx, sealed)
}

func (c *MockClient) GetNodeVersionInfo(ctx context.Context) (*flow.NodeVersionInfo, error) {
	return c.getNodeVersionInfoFunc(ctx)
}

func (c *MockClient) SubscribeEventsByBlockHeight(
	ctx context.Context,
	startHeight uint64,
	filter flow.EventFilter,
	opts ...access.SubscribeOption,
) (<-chan flow.BlockEvents, <-chan error, error) {
	return c.subscribeEventsByBlockHeightFunc(ctx, startHeight, filter, opts...)
}

func SetupClientForRange(startHeight uint64, endHeight uint64) access.Client {
	return &MockClient{
		Client: &mocks.Client{},
		getLatestBlockHeaderFunc: func(ctx context.Context, sealed bool) (*flow.BlockHeader, error) {
			return &flow.BlockHeader{
				Height: endHeight,
			}, nil
		},
		getBlockHeaderByHeightFunc: func(ctx context.Context, height uint64) (*flow.BlockHeader, error) {
			if height < startHeight || height > endHeight {
				return nil, storage.ErrNotFound
			}

			return &flow.BlockHeader{
				Height: height,
			}, nil
		},
		getNodeVersionInfoFunc: func(ctx context.Context) (*flow.NodeVersionInfo, error) {
			return &flow.NodeVersionInfo{
				NodeRootBlockHeight: startHeight,
			}, nil
		},
		subscribeEventsByBlockHeightFunc: func(
			ctx context.Context,
			startHeight uint64,
			filter flow.EventFilter,
			opts ...access.SubscribeOption,
		) (<-chan flow.BlockEvents, <-chan error, error) {
			events := make(chan flow.BlockEvents)

			go func() {
				defer close(events)

				for i := startHeight; i <= endHeight; i++ {
					events <- flow.BlockEvents{
						Height: i,
					}
				}
			}()

			return events, make(chan error), nil
		},
	}
}
