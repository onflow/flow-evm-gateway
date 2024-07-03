package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	"github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/engine/access/subscription"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/filters"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
	storageErrs "github.com/onflow/flow-evm-gateway/storage/errors"
)

// subscriptionBufferLimit is a constant that represents the buffer limit for subscriptions.
// It defines the maximum number of events that can be buffered in a subscription channel
const subscriptionBufferLimit = 1

type StreamAPI struct {
	logger                  zerolog.Logger
	config                  *config.Config
	blocks                  storage.BlockIndexer
	transactions            storage.TransactionIndexer
	receipts                storage.ReceiptIndexer
	blocksBroadcaster       *engine.Broadcaster
	transactionsBroadcaster *engine.Broadcaster
	logsBroadcaster         *engine.Broadcaster
	ratelimiter             limiter.Store
	blockPublisher          *models.Publisher
}

func NewStreamAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	blocksBroadcaster *engine.Broadcaster,
	transactionsBroadcaster *engine.Broadcaster,
	logsBroadcaster *engine.Broadcaster,
	ratelimiter limiter.Store,
) *StreamAPI {
	return &StreamAPI{
		logger:                  logger,
		config:                  config,
		blocks:                  blocks,
		transactions:            transactions,
		receipts:                receipts,
		blocksBroadcaster:       blocksBroadcaster,
		transactionsBroadcaster: transactionsBroadcaster,
		logsBroadcaster:         logsBroadcaster,
		ratelimiter:             ratelimiter,
	}
}

// NewHeads send a notification each time a new block is appended to the chain.
func (s *StreamAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	return s.new(ctx, func(notifier *rpc.Notifier, sub *rpc.Subscription) func(any) error {
		return func(data any) error {
			block, ok := data.(*types.Block)
			if !ok {
				return fmt.Errorf("invalid data sent to block subscription")
			}

			h, err := block.Hash()
			if err != nil {
				return err
			}

			return notifier.Notify(sub.ID, &Block{
				Hash:          h,
				Number:        hexutil.Uint64(block.Height),
				ParentHash:    block.ParentBlockHash,
				ReceiptsRoot:  block.ReceiptRoot,
				Transactions:  block.TransactionHashes,
				Uncles:        []common.Hash{},
				GasLimit:      hexutil.Uint64(blockGasLimit),
				Nonce:         gethTypes.BlockNonce{0x1},
				Timestamp:     hexutil.Uint64(block.Timestamp),
				BaseFeePerGas: hexutil.Big(*big.NewInt(0)),
			})
		}
	})
}

// NewPendingTransactions creates a subscription that is triggered each time a
// transaction enters the transaction pool. If fullTx is true the full tx is
// sent to the client, otherwise the hash is sent.
func (s *StreamAPI) NewPendingTransactions(ctx context.Context, fullTx *bool) (*rpc.Subscription, error) {
	sub, err := s.newSubscription(
		ctx,
		s.transactionsBroadcaster,
		func(ctx context.Context, height uint64) (interface{}, error) {
			block, err := s.blocks.GetByHeight(height)
			if err != nil {
				if errors.Is(err, storageErrs.ErrNotFound) {
					// make sure to wrap in not found error as the streamer expects it
					return nil, errors.Join(subscription.ErrBlockNotReady, err)
				}
				return nil, fmt.Errorf("failed to get block at height: %d: %w", height, err)
			}

			hashes := make([]common.Hash, len(block.TransactionHashes))
			txs := make([]*Transaction, len(block.TransactionHashes))

			for i, hash := range block.TransactionHashes {

				tx, err := s.transactions.Get(hash)
				if err != nil {
					if errors.Is(err, storageErrs.ErrNotFound) {
						// make sure to wrap in not found error as the streamer expects it
						return nil, errors.Join(subscription.ErrBlockNotReady, err)
					}
					return nil, fmt.Errorf("failed to get tx with hash: %s at height: %d: %w", hash, height, err)
				}

				rcp, err := s.receipts.GetByTransactionID(hash)
				if err != nil {
					if errors.Is(err, storageErrs.ErrNotFound) {
						// make sure to wrap in not found error as the streamer expects it
						return nil, errors.Join(subscription.ErrBlockNotReady, err)
					}
					return nil, fmt.Errorf("failed to get receipt with hash: %s at height: %d: %w", hash, height, err)
				}

				h := tx.Hash()

				t, err := NewTransaction(tx, *rcp, *s.config)
				if err != nil {
					return nil, err
				}

				hashes[i] = h
				txs[i] = t
			}

			if fullTx != nil && *fullTx {
				return txs, nil
			}
			return hashes, nil
		},
	)
	l := s.logger.With().Str("subscription-id", string(sub.ID)).Logger()
	if err != nil {
		l.Error().Err(err).Msg("error creating a new pending transactions subscription")
		return nil, err
	}

	l.Info().Msg("new pending transactions subscription created")
	return sub, nil
}

// Logs creates a subscription that fires for all new log that match the given filter criteria.
func (s *StreamAPI) Logs(ctx context.Context, criteria filters.FilterCriteria) (*rpc.Subscription, error) {
	filter, err := logs.NewFilterCriteria(criteria.Addresses, criteria.Topics)
	if err != nil {
		return nil, fmt.Errorf("failed to crete log subscription filter: %w", err)
	}

	sub, err := s.newSubscription(
		ctx,
		s.logsBroadcaster,
		func(ctx context.Context, height uint64) (interface{}, error) {
			if criteria.ToBlock != nil && height > criteria.ToBlock.Uint64() {
				return nil, nil
			}
			if criteria.FromBlock != nil && height < criteria.FromBlock.Uint64() {
				return nil, nil
			}

			block, err := s.blocks.GetByHeight(height)
			if err != nil {
				if errors.Is(err, storageErrs.ErrNotFound) {
					// make sure to wrap in not found error as the streamer expects it
					return nil, errors.Join(subscription.ErrBlockNotReady, err)
				}
				return nil, fmt.Errorf("failed to get block at height: %d: %w", height, err)
			}

			id, err := block.Hash()
			if err != nil {
				return nil, err
			}

			// todo change this to height filter so we don't have to get the same block twice, once for height and then for id

			return logs.NewIDFilter(id, *filter, s.blocks, s.receipts).Match()
		},
	)
	l := s.logger.With().Str("subscription-id", string(sub.ID)).Logger()
	if err != nil {
		l.Error().Err(err).Msg("failed creating logs subscription")
		return nil, err
	}

	l.Info().Msg("new logs subscription created")

	return sub, nil
}

// newSubscription creates a new subscription for receiving events from the broadcaster.
// The data adapter function is used to transform the raw data received from the broadcaster
// to comply with requested RPC API response schema.
func (s *StreamAPI) newSubscription(
	ctx context.Context,
	broadcaster *engine.Broadcaster,
	getData subscription.GetDataByHeightFunc,
) (*rpc.Subscription, error) {
	if err := rateLimit(ctx, s.ratelimiter, s.logger); err != nil {
		return nil, err
	}

	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}

	height, err := s.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}
	height += 1 // subscribe to the next new event which will produce next height

	sub := subscription.NewHeightBasedSubscription(subscriptionBufferLimit, height, getData)

	rpcSub := notifier.CreateSubscription()
	rpcSub.ID = rpc.ID(sub.ID()) // make sure ids are unified

	l := s.logger.With().Str("subscription-id", string(rpcSub.ID)).Logger()
	l.Info().Uint64("evm-height", height).Msg("new subscription created")

	go subscription.NewStreamer(
		s.logger.With().Str("component", "streamer").Logger(),
		broadcaster,
		s.config.StreamTimeout,
		s.config.StreamLimit,
		sub,
	).Stream(context.Background()) // todo investigate why the passed in context is canceled so quickly

	// stream all available data
	go streamData(notifier, rpcSub, sub, l)

	return rpcSub, nil
}

// streamData runs a loop listening on a chanel for any new data and if
// received it streams it to the rpc client.
func streamData(
	notifier *rpc.Notifier,
	rpcSub *rpc.Subscription,
	sub *subscription.HeightBasedSubscription,
	l zerolog.Logger,
) {
	defer sub.Close()

	for {
		select {
		case data, open := <-sub.Channel():
			if !open {
				l.Debug().Msg("subscription channel closed")
				return
			}

			// if we receive slice of data we stream each item separately
			// as it's expected by the rpc specification
			v := reflect.ValueOf(data)
			if v.Kind() == reflect.Slice {
				for i := 0; i < v.Len(); i++ {
					item := v.Index(i).Interface()
					sendData(notifier, rpcSub.ID, item, l)
				}
			} else {
				sendData(notifier, rpcSub.ID, data, l)
			}

		case err := <-rpcSub.Err():
			l.Debug().Err(err).Msg("client unsubscribed")
			return
		case <-notifier.Closed():
			l.Debug().Msg("client unsubscribed deprecated method")
			return
		}
	}
}

func sendData(notifier *rpc.Notifier, id rpc.ID, data any, l zerolog.Logger) {
	l.Debug().Str("subscription-id", string(id)).Any("data", data).Msg("notifying new event")

	if err := notifier.Notify(id, data); err != nil {
		l.Err(err).Msg("failed to notify")
	}
}

func (s *StreamAPI) new(
	ctx context.Context,
	callback func(notifier *rpc.Notifier, sub *rpc.Subscription) func(any) error,
) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	subs := models.NewSubscription(callback(notifier, rpcSub))

	rpcSub.ID = rpc.ID(subs.ID().String())
	l := s.logger.With().Str("subscription-id", subs.ID().String()).Logger()

	s.blockPublisher.Subscribe(subs)

	go func() {
		defer s.blockPublisher.Unsubscribe(subs)

		for {
			select {
			case err := <-subs.Error():
				fmt.Println(err)
				return
			case err := <-rpcSub.Err():
				l.Debug().Err(err).Msg("client unsubscribed")
				return
			case <-notifier.Closed():
				l.Debug().Msg("client unsubscribed deprecated method")
				return
			}
		}
	}()

	l.Info().Msg("new heads subscription created")

	return rpcSub, nil
}
