package api

import (
	"context"
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/filters"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
)

type StreamAPI struct {
	logger                zerolog.Logger
	config                *config.Config
	blocks                storage.BlockIndexer
	transactions          storage.TransactionIndexer
	receipts              storage.ReceiptIndexer
	blocksPublisher       *models.Publisher[*models.Block]
	transactionsPublisher *models.Publisher[*gethTypes.Transaction]
	logsPublisher         *models.Publisher[[]*gethTypes.Log]
}

func NewStreamAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	blocksPublisher *models.Publisher[*models.Block],
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logsPublisher *models.Publisher[[]*gethTypes.Log],
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
	}
}

// NewHeads send a notification each time a new block is appended to the chain.
func (s *StreamAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	return newSubscription(
		ctx,
		s.logger,
		s.blocksPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func(block *models.Block) error {
			return func(block *models.Block) error {
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
	return newSubscription(
		ctx,
		s.logger,
		s.transactionsPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func(*gethTypes.Transaction) error {
			return func(tx *gethTypes.Transaction) error {
				if fullTx != nil && *fullTx {
					return notifier.Notify(sub.ID, tx)
				}

				return notifier.Notify(sub.ID, tx.Hash())
			}
		},
	)
}

// Logs creates a subscription that fires for all new log that match the given filter criteria.
func (s *StreamAPI) Logs(ctx context.Context, criteria filters.FilterCriteria) (*rpc.Subscription, error) {
	logCriteria, err := logs.NewFilterCriteria(criteria.Addresses, criteria.Topics)
	if err != nil {
		return nil, fmt.Errorf("failed to create log subscription filter: %w", err)
	}

	return newSubscription(
		ctx,
		s.logger,
		s.logsPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func([]*gethTypes.Log) error {
			return func(allLogs []*gethTypes.Log) error {
				for _, log := range allLogs {
					// todo we could optimize this matching for cases where we have multiple subscriptions
					// using the same filter criteria, we could only filter once and stream to all subscribers
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

func newSubscription[T any](
	ctx context.Context,
	logger zerolog.Logger,
	publisher *models.Publisher[T],
	callback func(notifier *rpc.Notifier, sub *rpc.Subscription) func(T) error,
) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	subs := models.NewSubscription(logger, callback(notifier, rpcSub))

	l := logger.With().
		Str("gateway-subscription-id", fmt.Sprintf("%p", subs)).
		Str("ethereum-subscription-id", string(rpcSub.ID)).
		Logger()

	publisher.Subscribe(subs)

	go func() {
		defer publisher.Unsubscribe(subs)

		for {
			select {
			case err := <-subs.Error():
				l.Debug().Err(err).Msg("subscription returned error")
				return
			case err := <-rpcSub.Err():
				l.Debug().Err(err).Msg("client unsubscribed")
				return
			}
		}
	}()

	l.Info().Msg("new heads subscription created")

	return rpcSub, nil
}
