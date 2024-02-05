package events

import (
	"context"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var blockExecutedType = (types.EVMLocation{}).TypeID(nil, string(types.EventTypeBlockExecuted))
var txExecutedType = (types.EVMLocation{}).TypeID(nil, string(types.EventTypeTransactionExecuted))

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
			string(blockExecutedType),
			string(txExecutedType),
		},
	}

	return r.client.SubscribeEventsByBlockHeight(ctx, height, filter)
}
