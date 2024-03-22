package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/onflow/flow-evm-gateway/services/logs"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	errs "github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	storageErrs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/engine/access/state_stream/backend"
	flowgoStorage "github.com/onflow/flow-go/storage"
	"github.com/rs/zerolog"
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
	accounts                storage.AccountIndexer
	blocksBroadcaster       *engine.Broadcaster
	transactionsBroadcaster *engine.Broadcaster
	logsBroadcaster         *engine.Broadcaster
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
	logsBroadcaster *engine.Broadcaster,
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
		logsBroadcaster:         logsBroadcaster,
	}
}

// newSubscription creates a new subscription for receiving events from the broadcaster.
// The data adapter function is used to transform the raw data received from the broadcaster
// to comply with requested RPC API response schema.
func (s *StreamAPI) newSubscription(
	ctx context.Context,
	broadcaster *engine.Broadcaster,
	getData backend.GetDataByHeightFunc,
) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	height, err := s.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	sub := backend.NewHeightBasedSubscription(subscriptionBufferLimit, height, getData)

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
	).Stream(context.Background()) // todo investigate why the passed in context is canceled so quickly

	go func() {
		for {
			select {
			case data, open := <-sub.Channel():
				if !open {
					l.Debug().Msg("subscription channel closed")
					return
				}

				l.Debug().Msg("notifying new event")
				err = notifier.Notify(rpcSub.ID, data)
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
	return s.newSubscription(
		ctx,
		s.blocksBroadcaster,
		func(ctx context.Context, height uint64) (interface{}, error) {
			block, err := s.blocks.GetByHeight(height)
			if err != nil {
				if errors.Is(err, storageErrs.ErrNotFound) { // make sure to wrap in not found error as the streamer expects it
					return nil, errors.Join(flowgoStorage.ErrNotFound, err)
				}
				return nil, fmt.Errorf("failed to get block at height: %d: %w", height, err)
			}

			h, err := block.Hash()
			if err != nil {
				return nil, errs.ErrInternal
			}
			// todo there is a lot of missing data: https://docs.chainstack.com/reference/ethereum-native-subscribe-newheads
			return Block{
				Hash:         h,
				Number:       hexutil.Uint64(block.Height),
				ParentHash:   block.ParentBlockHash,
				ReceiptsRoot: block.ReceiptRoot,
				Transactions: block.TransactionHashes}, nil
		},
	)
}

// NewPendingTransactions creates a subscription that is triggered each time a
// transaction enters the transaction pool. If fullTx is true the full tx is
// sent to the client, otherwise the hash is sent.
func (s *StreamAPI) NewPendingTransactions(ctx context.Context, fullTx *bool) (*rpc.Subscription, error) {
	return s.newSubscription(
		ctx,
		s.transactionsBroadcaster,
		func(ctx context.Context, height uint64) (interface{}, error) {
			block, err := s.blocks.GetByHeight(height)
			if err != nil {
				return nil, fmt.Errorf("failed to get block at height: %d: %w", height, err)
			}

			// todo once a block can contain multiple transactions this needs to be refactored
			if len(block.TransactionHashes) != 1 {
				return nil, fmt.Errorf("block contains more than a single transaction")
			}
			hash := block.TransactionHashes[0]

			tx, err := s.transactions.Get(hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get tx with hash: %s at height: %d: %w", hash, height, err)
			}

			rcp, err := s.receipts.GetByTransactionID(hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get receipt with hash: %s at height: %d: %w", hash, height, err)
			}

			from, err := tx.From()
			if err != nil {
				return nil, errs.ErrInternal
			}

			h, err := tx.Hash()
			if err != nil {
				return nil, err
			}

			v, r, ss := tx.RawSignatureValues()

			var to string
			if tx.To() != nil {
				to = tx.To().Hex()
			}

			return &RPCTransaction{
				Hash:        h,
				BlockHash:   &rcp.BlockHash,
				BlockNumber: (*hexutil.Big)(rcp.BlockNumber),
				From:        from.Hex(),
				To:          to,
				Gas:         hexutil.Uint64(rcp.GasUsed),
				GasPrice:    (*hexutil.Big)(rcp.EffectiveGasPrice),
				Input:       tx.Data(),
				Nonce:       hexutil.Uint64(tx.Nonce()),
				Value:       (*hexutil.Big)(tx.Value()),
				Type:        hexutil.Uint64(tx.Type()),
				V:           (*hexutil.Big)(v),
				R:           (*hexutil.Big)(r),
				S:           (*hexutil.Big)(ss),
			}, nil
		},
	)
}

// Logs creates a subscription that fires for all new log that match the given filter criteria.
func (s *StreamAPI) Logs(ctx context.Context, criteria logs.FilterCriteria) (*rpc.Subscription, error) {

	return s.newSubscription(
		ctx,
		s.logsBroadcaster,
		func(ctx context.Context, height uint64) (interface{}, error) {
			block, err := s.blocks.GetByHeight(height)
			if err != nil {
				return nil, fmt.Errorf("failed to get block at height: %d: %w", height, err)
			}

			id, err := block.Hash()
			if err != nil {
				return nil, err
			}

			return logs.NewIDFilter(id, criteria, s.blocks, s.receipts).Match()
		},
	)
}
