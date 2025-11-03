package api

import (
	"context"
	"fmt"
	"slices"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
)

// The maximum number of transaction hash criteria allowed in a single subscription
const maxTxHashes = 200

type StreamAPI struct {
	logger                zerolog.Logger
	config                config.Config
	blocks                storage.BlockIndexer
	transactions          storage.TransactionIndexer
	receipts              storage.ReceiptIndexer
	blocksPublisher       *models.Publisher[*models.Block]
	transactionsPublisher *models.Publisher[*gethTypes.Transaction]
	receiptsPublisher     *models.Publisher[[]*models.Receipt]
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
	receiptsPublisher *models.Publisher[[]*models.Receipt],
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
		receiptsPublisher:     receiptsPublisher,
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
	if !logs.ValidCriteriaLimits(criteria) {
		return nil, errs.ErrExceedLogQueryLimit
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
					if !logs.ExactMatch(log, criteria) {
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

// TransactionReceipts creates a subscription that fires transaction
// receipts when transactions are included in blocks.
func (s *StreamAPI) TransactionReceipts(
	ctx context.Context,
	filter *filters.TransactionReceiptsQuery,
) (*rpc.Subscription, error) {
	// Validate transaction hashes limit
	if filter != nil && len(filter.TransactionHashes) > maxTxHashes {
		return nil, errs.ErrExceedMaxTxHashes
	}

	var txHashes []gethCommon.Hash

	if filter != nil {
		txHashes = filter.TransactionHashes
	}

	return newSubscription(
		ctx,
		s.logger,
		s.receiptsPublisher,
		func(notifier *rpc.Notifier, sub *rpc.Subscription) func([]*models.Receipt) error {
			return func(receipts []*models.Receipt) error {
				// Convert to the same format as `eth_getTransactionReceipt`
				marshaledReceipts := make([]map[string]any, 0)

				for _, receipt := range receipts {
					// Check if the subscription is only interested for a given
					// set of tx receipts.
					if len(txHashes) > 0 && !slices.Contains(txHashes, receipt.TxHash) {
						continue
					}

					tx, err := s.transactions.Get(receipt.TxHash)
					if err != nil {
						return err
					}

					txReceipt, err := ethTypes.MarshalReceipt(receipt, tx)
					if err != nil {
						return err
					}

					marshaledReceipts = append(marshaledReceipts, txReceipt)
				}

				// Send a batch of tx receipts in one notification
				return notifier.Notify(sub.ID, marshaledReceipts)
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
		return nil, err
	}

	blockHeader := &ethTypes.BlockHeader{
		Number:           hexutil.Uint64(block.Height),
		Hash:             h,
		ParentHash:       block.ParentBlockHash,
		Nonce:            gethTypes.BlockNonce{0x1},
		Sha3Uncles:       gethTypes.EmptyUncleHash,
		LogsBloom:        gethTypes.CreateBloom(&gethTypes.Receipt{}).Bytes(),
		TransactionsRoot: block.TransactionHashRoot,
		ReceiptsRoot:     block.ReceiptRoot,
		Miner:            evmTypes.CoinbaseAddress.ToCommon(),
		GasLimit:         hexutil.Uint64(BlockGasLimit),
		Timestamp:        hexutil.Uint64(block.Timestamp),
	}

	txHashes := block.TransactionHashes
	if len(txHashes) > 0 {
		totalGasUsed := hexutil.Uint64(0)
		receipts := gethTypes.Receipts{}
		for _, txHash := range txHashes {
			txReceipt, err := s.receipts.GetByTransactionID(txHash)
			if err != nil {
				return nil, err
			}
			totalGasUsed += hexutil.Uint64(txReceipt.GasUsed)
			receipts = append(receipts, txReceipt.ToGethReceipt())
		}
		blockHeader.GasUsed = totalGasUsed
		// TODO(m-Peter): Consider if its worthwhile to move this in storage.
		blockHeader.LogsBloom = gethTypes.MergeBloom(receipts).Bytes()
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
