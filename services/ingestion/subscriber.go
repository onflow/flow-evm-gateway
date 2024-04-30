package ingestion

import (
	"context"
	"fmt"
	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
)

type EventSubscriber interface {
	// Subscribe to relevant events from the provided block height.
	// Returns a channel with block events and errors,
	// if subscription fails returns an error as the third value.
	Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error)
}

type RPCSubscriber struct {
	client access.Client
	chain  flowGo.ChainID
}

func NewRPCSubscriber(client access.Client, chainID flowGo.ChainID) *RPCSubscriber {
	return &RPCSubscriber{client: client, chain: chainID}
}

func (r *RPCSubscriber) Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error) {
	// define events we subscribe to: A.{evm}.EVM.BlockExecuted and A.{evm}.EVM.TransactionExecuted,
	// where {evm} is EVM deployed contract address, which depends on the chain ID we configure
	evmAddress := common.Address(systemcontracts.SystemContractsForChain(r.chain).EVMContract.Address)
	blockExecutedEvent := common.NewAddressLocation(nil, evmAddress, string(types.EventTypeBlockExecuted)).ID()
	transactionExecutedEvent := common.NewAddressLocation(nil, evmAddress, string(types.EventTypeTransactionExecuted)).ID()

	filter := flow.EventFilter{
		EventTypes: []string{
			blockExecutedEvent,
			transactionExecutedEvent,
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
