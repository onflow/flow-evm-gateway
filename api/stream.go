package api

import (
	"context"
	"fmt"
	"math/big"

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
)

// subscriptionBufferLimit is a constant that represents the buffer limit for subscriptions.
// It defines the maximum number of events that can be buffered in a subscription channel
const subscriptionBufferLimit = 1

type StreamAPI struct {
	logger                zerolog.Logger
	config                *config.Config
	blocks                storage.BlockIndexer
	transactions          storage.TransactionIndexer
	receipts              storage.ReceiptIndexer
	blocksPublisher       *models.Publisher
	transactionsPublisher *models.Publisher
	logsPublisher         *models.Publisher
	ratelimiter           limiter.Store
}

func NewStreamAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	blocksPublisher *models.Publisher,
	transactionsPublisher *models.Publisher,
	logsPublisher *models.Publisher,
	ratelimiter limiter.Store,
) *StreamAPI {
	return &StreamAPI{
		logger:                logger,
		config:                config,
		blocks:                blocks,
		transactions:          transactions,
		receipts:              receipts,
		blocksPublisher:       blocksPublisher,
		transactionsPublisher: transactionsPublisher,
		logsPublisher:         logsPublisher,
		ratelimiter:           ratelimiter,
	}
}

// NewHeads send a notification each time a new block is appended to the chain.
func (s *StreamAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	return s.newSubscription(
		ctx,
		s.blocksPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func(any) error {
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
		},
	)
}

// NewPendingTransactions creates a subscription that is triggered each time a
// transaction enters the transaction pool. If fullTx is true the full tx is
// sent to the client, otherwise the hash is sent.
func (s *StreamAPI) NewPendingTransactions(ctx context.Context, fullTx *bool) (*rpc.Subscription, error) {
	return s.newSubscription(
		ctx,
		s.transactionsPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func(any) error {
			return func(data any) error {
				tx, ok := data.(models.Transaction)
				if !ok {
					return fmt.Errorf("invalid data sent to pending transaction subscription")
				}

				var res any
				if fullTx != nil && *fullTx {
					if r, err := NewTransaction(tx, s.config.EVMNetworkID); err != nil {
						return err
					} else {
						res = r
					}
				} else {
					res = tx.Hash()
				}

				return notifier.Notify(sub.ID, res)
			}
		},
	)
}

// Logs creates a subscription that fires for all new log that match the given filter criteria.
func (s *StreamAPI) Logs(ctx context.Context, criteria filters.FilterCriteria) (*rpc.Subscription, error) {
	logCriteria, err := logs.NewFilterCriteria(criteria.Addresses, criteria.Topics)
	if err != nil {
		return nil, fmt.Errorf("failed to crete log subscription filter: %w", err)
	}

	return s.newSubscription(
		ctx,
		s.logsPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func(any) error {
			return func(data any) error {
				allLogs, ok := data.([]*gethTypes.Log)
				if !ok {
					return fmt.Errorf("invalid data sent to log subscription")
				}

				for _, log := range allLogs {
					if !logs.ExactMatch(log, logCriteria) {
						continue
					}

					if err := notifier.Notify(sub.ID, log); err != nil {
						return err
					}
				}

				return nil
			}
		},
	)
}

func (s *StreamAPI) newSubscription(
	ctx context.Context,
	publisher *models.Publisher,
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

	publisher.Subscribe(subs)

	go func() {
		defer publisher.Unsubscribe(subs)

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
