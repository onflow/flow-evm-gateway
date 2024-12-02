package api

import (
	"context"
	"fmt"

	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common/hexutil"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/filters"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
)

type StreamAPI struct {
	logger                zerolog.Logger
	config                config.Config
	blocks                storage.BlockIndexer
	transactions          storage.TransactionIndexer
	receipts              storage.ReceiptIndexer
	blocksPublisher       *models.Publisher[*models.Block]
	transactionsPublisher *models.Publisher[*gethTypes.Transaction]
	logsPublisher         *models.Publisher[[]*gethTypes.Log]
}

func NewStreamAPI(
	logger zerolog.Logger,
	config config.Config,
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
				blockHeader, err := s.prepareBlockHeader(block)
				if err != nil {
					return fmt.Errorf("failed to get block header response: %w", err)
				}

				return notifier.Notify(sub.ID, blockHeader)
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

func (s *StreamAPI) prepareBlockHeader(
	block *models.Block,
) (*ethTypes.BlockHeader, error) {
	h, err := block.Hash()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to calculate hash for block by number")
		return nil, errs.ErrInternal
	}

	blockHeader := &ethTypes.BlockHeader{
		Number:           hexutil.Uint64(block.Height),
		Hash:             h,
		ParentHash:       block.ParentBlockHash,
		Nonce:            gethTypes.BlockNonce{0x1},
		Sha3Uncles:       gethTypes.EmptyUncleHash,
		LogsBloom:        gethTypes.LogsBloom([]*gethTypes.Log{}),
		TransactionsRoot: block.TransactionHashRoot,
		ReceiptsRoot:     block.ReceiptRoot,
		Miner:            evmTypes.CoinbaseAddress.ToCommon(),
		GasLimit:         hexutil.Uint64(BlockGasLimit),
		Timestamp:        hexutil.Uint64(block.Timestamp),
	}

	txHashes := block.TransactionHashes
	if len(txHashes) > 0 {
		totalGasUsed := hexutil.Uint64(0)
		logs := make([]*gethTypes.Log, 0)
		for _, txHash := range txHashes {
			txReceipt, err := s.receipts.GetByTransactionID(txHash)
			if err != nil {
				return nil, err
			}
			totalGasUsed += hexutil.Uint64(txReceipt.GasUsed)
			logs = append(logs, txReceipt.Logs...)
		}
		blockHeader.GasUsed = totalGasUsed
		// TODO(m-Peter): Consider if its worthwhile to move this in storage.
		blockHeader.LogsBloom = gethTypes.LogsBloom(logs)
	}

	return blockHeader, nil
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
