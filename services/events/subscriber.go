package events

import (
	"context"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
)

type Subscriber interface {
	// Subscribe to relevant events from the provided block height.
	// Returns a channel with block events and errors,
	// if subscription fails returns an error as the third value.
	Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error)
}

type RPCSubscriber struct {
	client access.Client
}

func (r *RPCSubscriber) Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error) {
	filter := flow.EventFilter{
		EventTypes: []string{
			string(models.BlockExecutedEventType),
			string(models.TransactionExecutedEventType),
		},
	}

	return r.client.SubscribeEventsByBlockHeight(ctx, height, filter)
}
