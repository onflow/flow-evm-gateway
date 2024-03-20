package api

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/engine/access/state_stream/backend"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
)

type StreamAPI struct {
	logger                  zerolog.Logger
	config                  *config.Config
	blocks                  storage.BlockIndexer
	transactions            storage.TransactionIndexer
	receipts                storage.ReceiptIndexer
	accounts                storage.AccountIndexer
	blocksBroadcaster       *engine.Broadcaster
	transactionsBroadcaster *engine.Broadcaster
}

func NewStreamAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	blocksBroadcaster *engine.Broadcaster,
	transactionsBroadcaster *engine.Broadcaster,
) *StreamAPI {
	return &StreamAPI{
		logger:                  logger,
		config:                  config,
		blocks:                  blocks,
		transactions:            transactions,
		receipts:                receipts,
		accounts:                accounts,
		blocksBroadcaster:       blocksBroadcaster,
		transactionsBroadcaster: transactionsBroadcaster,
	}
}

// newSubscription creates a new subscription for receiving events from the broadcaster.
// The data adapter function is used to transform the raw data received from the broadcaster
// to comply with requested RPC API response schema.
func (s *StreamAPI) newSubscription(
	ctx context.Context,
	broadcaster *engine.Broadcaster,
	dataAdapter func(any) (any, error),
) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	height, err := s.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	sub := backend.NewHeightBasedSubscription(
		1,
		height,
		func(ctx context.Context, height uint64) (interface{}, error) {
			return s.blocks.GetByHeight(height)
		},
	)

	rpcSub := notifier.CreateSubscription()
	rpcSub.ID = rpc.ID(sub.ID()) // make sure ids are unified

	l := s.logger.With().Str("subscription-id", string(rpcSub.ID)).Logger()
	l.Info().Msg("new subscription created")

	go backend.NewStreamer(
		s.logger.With().Str("component", "streamer").Logger(),
		broadcaster,
		s.config.StreamTimeout,
		s.config.StreamLimit,
		sub,
	).Stream(context.Background())

	go func() {
		for {
			select {
			case data, open := <-sub.Channel():
				if !open {
					l.Debug().Msg("subscription channel closed")
					return
				}

				adapted, err := dataAdapter(data)
				if err != nil {
					l.Err(err).Msg("failed to adapt data")
					continue
				}

				l.Debug().Msg("notifying new event")
				err = notifier.Notify(rpcSub.ID, adapted)
				if err != nil {
					l.Err(err).Msg("failed to notify")
				}
			case err := <-rpcSub.Err():
				l.Error().Err(err).Msg("error from rpc subscriber")
				sub.Close()
				return
			case <-notifier.Closed():
				sub.Close()
				return
			}
		}
	}()

	return rpcSub, nil
}

// NewHeads send a notification each time a new block is appended to the chain.
func (s *StreamAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	return s.newSubscription(ctx, s.blocksBroadcaster, func(blockData any) (any, error) {
		block, ok := blockData.(*types.Block)
		if !ok {
			return nil, fmt.Errorf("received data of incorrect type, it should be of type *types.Block")
		}

		h, err := block.Hash()
		if err != nil {
			return nil, err
		}
		// todo there is a lot of missing data: https://docs.chainstack.com/reference/ethereum-native-subscribe-newheads
		return Block{
			Hash:         h,
			Number:       hexutil.Uint64(block.Height),
			ParentHash:   block.ParentBlockHash,
			ReceiptsRoot: block.ReceiptRoot,
		}, nil
	})
}

// NewPendingTransactions creates a subscription that is triggered each time a
// transaction enters the transaction pool. If fullTx is true the full tx is
// sent to the client, otherwise the hash is sent.
func (s *StreamAPI) NewPendingTransactions(ctx context.Context, fullTx *bool) (*rpc.Subscription, error) {
	return s.newSubscription(ctx, s.transactionsBroadcaster, func(txData any) (any, error) {
		return txData, nil
	})
}
