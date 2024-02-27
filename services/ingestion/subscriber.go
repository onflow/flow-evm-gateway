package ingestion

import (
	"context"
	"fmt"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var blockExecutedType = (flow.EVMLocation{}).TypeID(nil, string(types.EventTypeBlockExecuted))
var txExecutedType = (flow.EVMLocation{}).TypeID(nil, string(types.EventTypeTransactionExecuted))

type EventSubscriber interface {
	// Subscribe to relevant events from the provided block height.
	// Returns a channel with block events and errors,
	// if subscription fails returns an error as the third value.
	Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error)
}

type RPCSubscriber struct {
	client access.Client
}

func NewRPCSubscriber(client access.Client) *RPCSubscriber {
	return &RPCSubscriber{client: client}
}

func (r *RPCSubscriber) Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error) {
	filter := flow.EventFilter{
		EventTypes: []string{
			string(blockExecutedType),
			string(txExecutedType),
		},
	}

	_, err := r.client.GetBlockByHeight(ctx, height)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to subscribe for events, the block height %d doesn't exist: %w", height, err)
	}

	// todo revisit if we should use custom heartbeat interval grpc.WithHeartbeatInterval(1)
	evs, errs, err := r.client.SubscribeEventsByBlockHeight(ctx, height, filter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to subscribe to events by block height: %w", err)
	}

	return evs, errs, nil
}
