package api

import (
	"context"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/engine/access/state_stream/backend"
	"github.com/rs/zerolog"
)

type StreamAPI struct {
	logger            zerolog.Logger
	config            *config.Config
	blocks            storage.BlockIndexer
	transactions      storage.TransactionIndexer
	receipts          storage.ReceiptIndexer
	accounts          storage.AccountIndexer
	blocksBroadcaster *engine.Broadcaster
}

func NewStreamAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	blocksBroadcaster *engine.Broadcaster,
) *StreamAPI {
	return &StreamAPI{
		logger:            logger,
		config:            config,
		blocks:            blocks,
		transactions:      transactions,
		receipts:          receipts,
		accounts:          accounts,
		blocksBroadcaster: blocksBroadcaster,
	}
}

func (s *StreamAPI) newSubscription(ctx context.Context, broadcaster *engine.Broadcaster) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	height, err := s.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	sub := backend.NewHeightBasedSubscription(1, height, func(ctx context.Context, height uint64) (interface{}, error) {
		return s.blocks.GetByHeight(height)
	})

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
			case block, open := <-sub.Channel():
				if !open {
					l.Debug().Msg("subscription channel closed")
					return
				}

				l.Debug().Msg("notifying new head event")
				err = notifier.Notify(rpcSub.ID, block)
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

func (s *StreamAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	// todo make sure the returned data is in fact correct: https://docs.chainstack.com/reference/ethereum-native-subscribe-newheads
	return s.newSubscription(ctx, s.blocksBroadcaster)
}
