package api

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	"github.com/onflow/go-ethereum/common/math"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/filters"
	"github.com/onflow/go-ethereum/rlp"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"
	"github.com/sethvargo/go-limiter"

	evmTypes "github.com/onflow/flow-go/fvm/evm/types"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
)

const maxFeeHistoryBlockCount = 1024

var latestBlockNumberOrHash = rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)

func SupportedAPIs(
	blockChainAPI *BlockChainAPI,
	streamAPI *StreamAPI,
	pullAPI *PullAPI,
	debugAPI *DebugAPI,
	walletAPI *WalletAPI,
	config *config.Config,
) []rpc.API {
	apis := []rpc.API{{
		Namespace: "eth",
		Service:   blockChainAPI,
	}, {
		Namespace: "eth",
		Service:   streamAPI,
	}, {
		Namespace: "eth",
		Service:   pullAPI,
	}, {
		Namespace: "web3",
		Service:   &Web3API{},
	}, {
		Namespace: "net",
		Service:   NewNetAPI(config),
	}, {
		Namespace: "txpool",
		Service:   NewTxPoolAPI(),
	}}

	// optional debug api
	if debugAPI != nil {
		apis = append(apis, rpc.API{
			Namespace: "debug",
			Service:   debugAPI,
		})
	}

	if walletAPI != nil {
		apis = append(apis, rpc.API{
			Namespace: "eth",
			Service:   walletAPI,
		})
	}

	return apis
}

type BlockChainAPI struct {
	logger                zerolog.Logger
	config                *config.Config
	evm                   requester.EVMClient
	blocks                storage.BlockIndexer
	transactions          storage.TransactionIndexer
	receipts              storage.ReceiptIndexer
	accounts              storage.AccountIndexer
	indexingResumedHeight uint64
	limiter               limiter.Store
	collector             metrics.Collector
}

func NewBlockChainAPI(
	logger zerolog.Logger,
	config *config.Config,
	evm requester.EVMClient,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
	ratelimiter limiter.Store,
	collector metrics.Collector,
) (*BlockChainAPI, error) {
	// get the height from which the indexing resumed since the last restart, this is needed for syncing status.
	indexingResumedHeight, err := blocks.LatestEVMHeight()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve the indexing resumed height: %w", err)
	}

	return &BlockChainAPI{
		logger:                logger,
		config:                config,
		evm:                   evm,
		blocks:                blocks,
		transactions:          transactions,
		receipts:              receipts,
		accounts:              accounts,
		indexingResumedHeight: indexingResumedHeight,
		limiter:               ratelimiter,
		collector:             collector,
	}, nil
}

// BlockNumber returns the block number of the chain head.
func (b *BlockChainAPI) BlockNumber(ctx context.Context) (hexutil.Uint64, error) {
	if err := rateLimit(ctx, b.limiter, b.logger); err != nil {
		return 0, err
	}

	latestBlockHeight, err := b.blocks.LatestEVMHeight()
	if err != nil {
		return handleError[hexutil.Uint64](err, b.logger, b.collector)
	}

	return hexutil.Uint64(latestBlockHeight), nil
}

// Syncing returns false in case the node is currently not syncing with the network.
// It can be up-to-date or has not yet received the latest block headers from its peers.
// In case it is synchronizing:
// - startingBlock: block number this node started to synchronize from
// - currentBlock:  block number this node is currently importing
// - highestBlock:  block number of the highest block header this node has received from peers
func (b *BlockChainAPI) Syncing(ctx context.Context) (interface{}, error) {
	if err := rateLimit(ctx, b.limiter, b.logger); err != nil {
		return nil, err
	}

	currentBlock, err := b.blocks.LatestEVMHeight()
	if err != nil {
		return handleError[any](err, b.logger, b.collector)
	}

	highestBlock, err := b.evm.GetLatestEVMHeight(ctx)
	if err != nil {
		return handleError[any](err, b.logger, b.collector)
	}

	if currentBlock == highestBlock {
		return false, nil
	}

	return SyncStatus{
		StartingBlock: hexutil.Uint64(b.indexingResumedHeight),
		CurrentBlock:  hexutil.Uint64(currentBlock),
		HighestBlock:  hexutil.Uint64(highestBlock),
	}, nil
}

// SendRawTransaction will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce.
func (b *BlockChainAPI) SendRawTransaction(
	ctx context.Context,
	input hexutil.Bytes,
) (common.Hash, error) {
	if b.config.IndexOnly {
		return common.Hash{}, errs.ErrIndexOnlyMode
	}

	l := b.logger.With().
		Str("endpoint", "sendRawTransaction").
		Str("input", input.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return common.Hash{}, err
	}

	id, err := b.evm.SendRawTransaction(ctx, input)
	if err != nil {
		return handleError[common.Hash](err, l, b.collector)
	}

	return id, nil
}

// GetBalance returns the amount of wei for the given address in the state of the
// given block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta
// block numbers are also allowed.
func (b *BlockChainAPI) GetBalance(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*hexutil.Big, error) {
	l := b.logger.With().
		Str("endpoint", "getBalance").
		Str("address", address.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	evmHeight, err := b.getBlockNumber(&blockNumberOrHash)
	if err != nil {
		return handleError[*hexutil.Big](err, l, b.collector)
	}

	balance, err := b.evm.GetBalance(ctx, address, evmHeight)
	if err != nil {
		return handleError[*hexutil.Big](err, l, b.collector)
	}

	return (*hexutil.Big)(balance), nil
}

// GetTransactionByHash returns the transaction for the given hash
func (b *BlockChainAPI) GetTransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*Transaction, error) {
	l := b.logger.With().
		Str("endpoint", "getTransactionByHash").
		Str("hash", hash.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	tx, err := b.transactions.Get(hash)
	if err != nil {
		return handleError[*Transaction](err, l, b.collector)
	}

	rcp, err := b.receipts.GetByTransactionID(hash)
	if err != nil {
		return handleError[*Transaction](err, l, b.collector)
	}

	return NewTransactionResult(tx, *rcp, b.config.EVMNetworkID)
}

// GetTransactionByBlockHashAndIndex returns the transaction for the given block hash and index.
func (b *BlockChainAPI) GetTransactionByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) (*Transaction, error) {
	l := b.logger.With().
		Str("endpoint", "getTransactionByBlockHashAndIndex").
		Str("hash", blockHash.String()).
		Str("index", index.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	block, err := b.blocks.GetByID(blockHash)
	if err != nil {
		return handleError[*Transaction](err, l, b.collector)
	}

	if int(index) >= len(block.TransactionHashes) {
		return nil, nil
	}

	txHash := block.TransactionHashes[index]
	tx, err := b.prepareTransactionResponse(txHash)
	if err != nil {
		return handleError[*Transaction](err, l, b.collector)
	}

	return tx, nil
}

// GetTransactionByBlockNumberAndIndex returns the transaction
// for the given block number and index.
func (b *BlockChainAPI) GetTransactionByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) (*Transaction, error) {
	l := b.logger.With().
		Str("endpoint", "getTransactionByBlockNumberAndIndex").
		Str("number", blockNumber.String()).
		Str("index", index.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	if blockNumber < rpc.EarliestBlockNumber {
		latestBlockNumber, err := b.blocks.LatestEVMHeight()
		if err != nil {
			return handleError[*Transaction](err, l, b.collector)
		}
		blockNumber = rpc.BlockNumber(latestBlockNumber)
	}

	block, err := b.blocks.GetByHeight(uint64(blockNumber))
	if err != nil {
		return handleError[*Transaction](err, l, b.collector)
	}

	if int(index) >= len(block.TransactionHashes) {
		return nil, nil
	}

	txHash := block.TransactionHashes[index]
	tx, err := b.prepareTransactionResponse(txHash)
	if err != nil {
		return handleError[*Transaction](err, l, b.collector)
	}

	return tx, nil
}

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (b *BlockChainAPI) GetTransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (map[string]interface{}, error) {
	l := b.logger.With().
		Str("endpoint", "getTransactionReceipt").
		Str("hash", hash.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	tx, err := b.transactions.Get(hash)
	if err != nil {
		return handleError[map[string]interface{}](err, l, b.collector)
	}

	receipt, err := b.receipts.GetByTransactionID(hash)
	if err != nil {
		return handleError[map[string]interface{}](err, l, b.collector)
	}

	txReceipt, err := MarshalReceipt(receipt, tx)
	if err != nil {
		return handleError[map[string]interface{}](err, l, b.collector)
	}

	return txReceipt, nil
}

// GetBlockByHash returns the requested block. When fullTx is true all transactions in the block are returned in full
// detail, otherwise only the transaction hash is returned.
func (b *BlockChainAPI) GetBlockByHash(
	ctx context.Context,
	hash common.Hash,
	fullTx bool,
) (*Block, error) {
	l := b.logger.With().
		Str("endpoint", "getBlockByHash").
		Str("hash", hash.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	block, err := b.blocks.GetByID(hash)
	if err != nil {
		return handleError[*Block](err, l, b.collector)
	}

	apiBlock, err := b.prepareBlockResponse(block, fullTx)
	if err != nil {
		return handleError[*Block](err, l, b.collector)
	}

	return apiBlock, nil
}

// GetBlockByNumber returns the requested canonical block.
//   - When blockNr is -1 the chain pending block is returned.
//   - When blockNr is -2 the chain latest block is returned.
//   - When blockNr is -3 the chain finalized block is returned.
//   - When blockNr is -4 the chain safe block is returned.
//   - When fullTx is true all transactions in the block are returned, otherwise
//     only the transaction hash is returned.
func (b *BlockChainAPI) GetBlockByNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	fullTx bool,
) (*Block, error) {
	l := b.logger.With().
		Str("endpoint", "getBlockByNumber").
		Str("blockNumber", blockNumber.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	height := uint64(blockNumber)
	var err error
	if blockNumber < 0 {
		height, err = b.blocks.LatestEVMHeight()
		if err != nil {
			return handleError[*Block](err, l, b.collector)
		}
	}

	block, err := b.blocks.GetByHeight(height)

	if err != nil {
		return handleError[*Block](err, l, b.collector)
	}

	apiBlock, err := b.prepareBlockResponse(block, fullTx)
	if err != nil {
		return handleError[*Block](err, l, b.collector)
	}

	return apiBlock, nil
}

// GetBlockReceipts returns the block receipts for the given block hash or number or tag.
func (b *BlockChainAPI) GetBlockReceipts(
	ctx context.Context,
	blockNumberOrHash rpc.BlockNumberOrHash,
) ([]map[string]interface{}, error) {
	l := b.logger.With().
		Str("endpoint", "getBlockReceipts").
		Str("hash", blockNumberOrHash.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	var (
		block *models.Block
		err   error
	)
	if blockNumberOrHash.BlockHash != nil {
		block, err = b.blocks.GetByID(*blockNumberOrHash.BlockHash)
	} else if blockNumberOrHash.BlockNumber != nil {
		block, err = b.blocks.GetByHeight(uint64(blockNumberOrHash.BlockNumber.Int64()))
	} else {
		return handleError[[]map[string]interface{}](
			fmt.Errorf("%w: block number or hash not provided", errs.ErrInvalid),
			l,
			b.collector,
		)
	}
	if err != nil {
		return handleError[[]map[string]interface{}](err, l, b.collector)
	}

	receipts := make([]map[string]interface{}, len(block.TransactionHashes))
	for i, hash := range block.TransactionHashes {
		tx, err := b.transactions.Get(hash)
		if err != nil {
			return handleError[[]map[string]interface{}](err, l, b.collector)
		}

		receipt, err := b.receipts.GetByTransactionID(hash)
		if err != nil {
			return handleError[[]map[string]interface{}](err, l, b.collector)
		}

		receipts[i], err = MarshalReceipt(receipt, tx)
		if err != nil {
			return handleError[[]map[string]interface{}](err, l, b.collector)
		}
	}

	return receipts, nil
}

// GetBlockTransactionCountByHash returns the number of transactions
// in the block with the given hash.
func (b *BlockChainAPI) GetBlockTransactionCountByHash(
	ctx context.Context,
	blockHash common.Hash,
) (*hexutil.Uint, error) {
	l := b.logger.With().
		Str("endpoint", "getBlockTransactionCountByHash").
		Str("hash", blockHash.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	block, err := b.blocks.GetByID(blockHash)
	if err != nil {
		return handleError[*hexutil.Uint](err, l, b.collector)
	}

	count := hexutil.Uint(len(block.TransactionHashes))
	return &count, nil
}

// GetBlockTransactionCountByNumber returns the number of transactions
// in the block with the given block number.
func (b *BlockChainAPI) GetBlockTransactionCountByNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
) (*hexutil.Uint, error) {
	l := b.logger.With().
		Str("endpoint", "getBlockTransactionCountByNumber").
		Str("number", blockNumber.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	if blockNumber < rpc.EarliestBlockNumber {
		latestBlockNumber, err := b.blocks.LatestEVMHeight()
		if err != nil {
			return handleError[*hexutil.Uint](err, l, b.collector)
		}
		blockNumber = rpc.BlockNumber(latestBlockNumber)
	}

	block, err := b.blocks.GetByHeight(uint64(blockNumber))
	if err != nil {
		return handleError[*hexutil.Uint](err, l, b.collector)
	}

	count := hexutil.Uint(len(block.TransactionHashes))
	return &count, nil
}

// Call executes the given transaction on the state for the given block number.
// Additionally, the caller can specify a batch of contract for fields overriding.
// Note, this function doesn't make and changes in the state/blockchain and is
// useful to execute and retrieve values.
func (b *BlockChainAPI) Call(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
	overrides *StateOverride,
	blockOverrides *BlockOverrides,
) (hexutil.Bytes, error) {
	l := b.logger.With().
		Str("endpoint", "call").
		Str("args", fmt.Sprintf("%v", args)).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	err := args.Validate()
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	// Default to "latest" block tag
	if blockNumberOrHash == nil {
		blockNumberOrHash = &latestBlockNumberOrHash
	}

	evmHeight, err := b.getBlockNumber(blockNumberOrHash)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	tx, err := encodeTxFromArgs(args)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	// Default address in case user does not provide one
	from := b.config.Coinbase
	if args.From != nil {
		from = *args.From
	}

	res, err := b.evm.Call(ctx, tx, from, evmHeight)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	return res, nil
}

// GetLogs returns logs matching the given argument that are stored within the state.
func (b *BlockChainAPI) GetLogs(
	ctx context.Context,
	criteria filters.FilterCriteria,
) ([]*types.Log, error) {
	l := b.logger.With().
		Str("endpoint", "getLogs").
		Str("criteria", fmt.Sprintf("%v", criteria)).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	filter := logs.FilterCriteria{
		Addresses: criteria.Addresses,
		Topics:    criteria.Topics,
	}

	// if filter provided specific block ID
	if criteria.BlockHash != nil {
		f, err := logs.NewIDFilter(*criteria.BlockHash, filter, b.blocks, b.receipts)
		if err != nil {
			return handleError[[]*types.Log](err, l, b.collector)
		}

		res, err := f.Match()
		if err != nil {
			return handleError[[]*types.Log](err, l, b.collector)
		}

		return res, nil
	}

	// otherwise we use the block range as the filter

	// assign default values to latest block number, unless provided
	from := models.LatestBlockNumber
	if criteria.FromBlock != nil {
		from = criteria.FromBlock
	}
	to := models.LatestBlockNumber
	if criteria.ToBlock != nil {
		to = criteria.ToBlock
	}

	h, err := b.blocks.LatestEVMHeight()
	if err != nil {
		return handleError[[]*types.Log](err, l, b.collector)
	}
	latest := big.NewInt(int64(h))

	// if special value, use latest block number
	if from.Cmp(models.EarliestBlockNumber) < 0 {
		from = latest
	}
	if to.Cmp(models.EarliestBlockNumber) < 0 {
		to = latest
	}

	f, err := logs.NewRangeFilter(from.Uint64(), to.Uint64(), filter, b.receipts)
	if err != nil {
		return handleError[[]*types.Log](err, l, b.collector)
	}

	res, err := f.Match()
	if err != nil {
		return handleError[[]*types.Log](err, l, b.collector)
	}

	// makes sure the response is correctly serialized
	if res == nil {
		return []*types.Log{}, nil
	}

	return res, nil
}

// GetTransactionCount returns the number of transactions the given address
// has sent for the given block number.
func (b *BlockChainAPI) GetTransactionCount(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*hexutil.Uint64, error) {
	l := b.logger.With().
		Str("endpoint", "getTransactionCount").
		Str("address", address.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	evmHeight, err := b.getBlockNumber(&blockNumberOrHash)
	if err != nil {
		return handleError[*hexutil.Uint64](err, l, b.collector)
	}

	networkNonce, err := b.evm.GetNonce(ctx, address, evmHeight)
	if err != nil {
		return handleError[*hexutil.Uint64](err, l, b.collector)
	}

	nonce, err := b.accounts.GetNonce(address)
	if err != nil {
		return handleError[*hexutil.Uint64](errs.ErrInternal, l, b.collector)
	}

	// compare both until we gain confidence in db nonce tracking working correctly
	if nonce != networkNonce {
		l.Error().
			Uint64("network-nonce", networkNonce).
			Uint64("db-nonce", nonce).
			Msg("network nonce does not equal db nonce")
	}

	return (*hexutil.Uint64)(&networkNonce), nil
}

// EstimateGas returns the lowest possible gas limit that allows the transaction to run
// successfully at block `blockNrOrHash`, or the latest block if `blockNrOrHash` is unspecified. It
// returns error if the transaction would revert or if there are unexpected failures. The returned
// value is capped by both `args.Gas` (if non-nil & non-zero) and the backend's RPCGasCap
// configuration (if non-zero).
func (b *BlockChainAPI) EstimateGas(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
	overrides *StateOverride,
) (hexutil.Uint64, error) {
	l := b.logger.With().
		Str("endpoint", "estimateGas").
		Str("args", fmt.Sprintf("%v", args)).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return 0, err
	}

	err := args.Validate()
	if err != nil {
		return handleError[hexutil.Uint64](err, l, b.collector)
	}

	tx, err := encodeTxFromArgs(args)
	if err != nil {
		return hexutil.Uint64(blockGasLimit), nil // return block gas limit
	}

	// Default address in case user does not provide one
	from := b.config.Coinbase
	if args.From != nil {
		from = *args.From
	}

	if blockNumberOrHash == nil {
		blockNumberOrHash = &latestBlockNumberOrHash
	}

	evmHeight, err := b.getBlockNumber(blockNumberOrHash)
	if err != nil {
		return handleError[hexutil.Uint64](err, l, b.collector)
	}

	estimatedGas, err := b.evm.EstimateGas(ctx, tx, from, evmHeight)
	if err != nil {
		return handleError[hexutil.Uint64](err, l, b.collector)
	}

	return hexutil.Uint64(estimatedGas), nil
}

// GetCode returns the code stored at the given address in
// the state for the given block number.
func (b *BlockChainAPI) GetCode(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	l := b.logger.With().
		Str("endpoint", "getCode").
		Str("address", address.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	evmHeight, err := b.getBlockNumber(&blockNumberOrHash)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	code, err := b.evm.GetCode(ctx, address, evmHeight)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	return code, nil
}

// FeeHistory returns transaction base fee per gas and effective priority fee
// per gas for the requested/supported block range.
// blockCount: Requested range of blocks. Clients will return less than the
// requested range if not all blocks are available.
// lastBlock: Highest block of the requested range.
// rewardPercentiles: A monotonically increasing list of percentile values.
// For each block in the requested range, the transactions will be sorted in
// ascending order by effective tip per gas and the coresponding effective tip
// for the percentile will be determined, accounting for gas consumed.
func (b *BlockChainAPI) FeeHistory(
	ctx context.Context,
	blockCount math.HexOrDecimal64,
	lastBlock rpc.BlockNumber,
	rewardPercentiles []float64,
) (*FeeHistoryResult, error) {
	l := b.logger.With().
		Str("endpoint", "feeHistory").
		Str("block", lastBlock.String()).
		Logger()

	if blockCount > maxFeeHistoryBlockCount {
		return handleError[*FeeHistoryResult](
			fmt.Errorf("block count has to be between 1 and %d, got: %d", maxFeeHistoryBlockCount, blockCount),
			l,
			b.collector,
		)
	}

	lastBlockNumber := uint64(lastBlock)
	var err error
	if lastBlock < 0 {
		// From the special block tags, we only support "latest".
		lastBlockNumber, err = b.blocks.LatestEVMHeight()
		if err != nil {
			return handleError[*FeeHistoryResult](err, l, b.collector)
		}
	}

	var (
		oldestBlock   *hexutil.Big
		baseFees      []*hexutil.Big
		rewards       [][]*hexutil.Big
		gasUsedRatios []float64
	)

	maxCount := uint64(blockCount)
	if maxCount > lastBlockNumber {
		maxCount = lastBlockNumber
	}

	blockRewards := make([]*hexutil.Big, len(rewardPercentiles))
	for i := range rewardPercentiles {
		blockRewards[i] = (*hexutil.Big)(b.config.GasPrice)
	}

	for i := maxCount; i >= uint64(1); i-- {
		// If the requested block count is 5, and the last block number
		// is 20, then we need the blocks [16, 17, 18, 19, 20] in this
		// specific order. The first block we fetch is 20 - 5 + 1 = 16.
		blockHeight := lastBlockNumber - i + 1
		block, err := b.blocks.GetByHeight(blockHeight)
		if err != nil {
			continue
		}

		if i == maxCount {
			oldestBlock = (*hexutil.Big)(big.NewInt(int64(block.Height)))
		}

		baseFees = append(baseFees, (*hexutil.Big)(big.NewInt(0)))

		rewards = append(rewards, blockRewards)

		gasUsedRatio := float64(block.TotalGasUsed) / float64(blockGasLimit)
		gasUsedRatios = append(gasUsedRatios, gasUsedRatio)
	}

	return &FeeHistoryResult{
		OldestBlock:  oldestBlock,
		Reward:       rewards,
		BaseFee:      baseFees,
		GasUsedRatio: gasUsedRatios,
	}, nil
}

// GetStorageAt returns the storage from the state at the given address, key and
// block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta block
// numbers are also allowed.
func (b *BlockChainAPI) GetStorageAt(
	ctx context.Context,
	address common.Address,
	storageSlot string,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	l := b.logger.With().
		Str("endpoint", "getStorageAt").
		Str("address", address.String()).
		Logger()

	if err := rateLimit(ctx, b.limiter, l); err != nil {
		return nil, err
	}

	key, _, err := decodeHash(storageSlot)
	if err != nil {
		return handleError[hexutil.Bytes](
			fmt.Errorf("%w: %w", errs.ErrInvalid, err),
			l,
			b.collector,
		)
	}

	evmHeight, err := b.getBlockNumber(&blockNumberOrHash)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	result, err := b.evm.GetStorageAt(ctx, address, key, evmHeight)
	if err != nil {
		return handleError[hexutil.Bytes](err, l, b.collector)
	}

	return result[:], nil
}

func (b *BlockChainAPI) fetchBlockTransactions(
	block *models.Block,
) ([]*Transaction, error) {
	transactions := make([]*Transaction, 0)
	for _, txHash := range block.TransactionHashes {
		transaction, err := b.prepareTransactionResponse(txHash)
		if err != nil {
			return nil, err
		}
		if transaction == nil {
			b.logger.Error().
				Str("tx-hash", txHash.String()).
				Uint64("evm-height", block.Height).
				Msg("not found a transaction the block references")

			continue
		}
		transactions = append(transactions, transaction)
	}

	return transactions, nil
}

func (b *BlockChainAPI) prepareTransactionResponse(
	txHash common.Hash,
) (*Transaction, error) {
	tx, err := b.transactions.Get(txHash)
	if err != nil {
		return nil, err
	}

	receipt, err := b.receipts.GetByTransactionID(txHash)
	if err != nil {
		return nil, err
	}

	return NewTransactionResult(tx, *receipt, b.config.EVMNetworkID)
}

func (b *BlockChainAPI) prepareBlockResponse(
	block *models.Block,
	fullTx bool,
) (*Block, error) {
	h, err := block.Hash()
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to calculate hash for block by number")
		return nil, errs.ErrInternal
	}

	blockResponse := &Block{
		Hash:             h,
		Number:           hexutil.Uint64(block.Height),
		ParentHash:       block.ParentBlockHash,
		ReceiptsRoot:     block.ReceiptRoot,
		TransactionsRoot: block.TransactionHashRoot,
		Transactions:     block.TransactionHashes,
		Uncles:           []common.Hash{},
		GasLimit:         hexutil.Uint64(blockGasLimit),
		Nonce:            types.BlockNonce{0x1},
		Timestamp:        hexutil.Uint64(block.Timestamp),
		BaseFeePerGas:    hexutil.Big(*big.NewInt(0)),
		LogsBloom:        types.LogsBloom([]*types.Log{}),
		Miner:            evmTypes.CoinbaseAddress.ToCommon(),
		Sha3Uncles:       types.EmptyUncleHash,
	}

	blockBytes, err := block.ToBytes()
	if err != nil {
		return nil, err
	}
	blockSize := rlp.ListSize(uint64(len(blockBytes)))

	transactions, err := b.fetchBlockTransactions(block)
	if err != nil {
		return nil, err
	}

	if len(transactions) > 0 {
		totalGasUsed := hexutil.Uint64(0)
		logs := make([]*types.Log, 0)
		for _, tx := range transactions {
			txReceipt, err := b.receipts.GetByTransactionID(tx.Hash)
			if err != nil {
				return nil, err
			}
			totalGasUsed += hexutil.Uint64(txReceipt.GasUsed)
			logs = append(logs, txReceipt.Logs...)
			blockSize += tx.Size()
		}
		blockResponse.GasUsed = totalGasUsed
		// TODO(m-Peter): Consider if its worthwhile to move this in storage.
		blockResponse.LogsBloom = types.LogsBloom(logs)
	}
	blockResponse.Size = hexutil.Uint64(rlp.ListSize(blockSize))

	if fullTx {
		blockResponse.Transactions = transactions
	}

	return blockResponse, nil
}

func (b *BlockChainAPI) getBlockNumber(blockNumberOrHash *rpc.BlockNumberOrHash) (int64, error) {
	err := fmt.Errorf("%w: neither block number nor hash specified", errs.ErrInvalid)
	if blockNumberOrHash == nil {
		return 0, err
	}
	if number, ok := blockNumberOrHash.Number(); ok {
		return number.Int64(), nil
	}

	if hash, ok := blockNumberOrHash.Hash(); ok {
		evmHeight, err := b.blocks.GetHeightByID(hash)
		if err != nil {
			b.logger.Error().Err(err).Msg("failed to get block by hash")
			return 0, err
		}
		return int64(evmHeight), nil
	}

	return 0, err
}

// handleError takes in an error and in case the error is of type ErrEntityNotFound
// it returns nil instead of an error since that is according to the API spec,
// if the error is not of type ErrEntityNotFound it will return the error and the generic
// empty type.
func handleError[T any](err error, log zerolog.Logger, collector metrics.Collector) (T, error) {
	var (
		zero        T
		revertedErr *errs.RevertError
	)

	switch {
	// as per specification returning nil and nil for not found resources
	case errors.Is(err, errs.ErrEntityNotFound):
		return zero, nil
	case errors.Is(err, errs.ErrInvalid):
		return zero, err
	case errors.Is(err, errs.ErrFailedTransaction):
		return zero, err
	case errors.As(err, &revertedErr):
		return zero, revertedErr
	default:
		collector.ApiErrorOccurred()
		log.Error().Err(err).Msg("api error")
		return zero, errs.ErrInternal
	}
}

// decodeHash parses a hex-encoded 32-byte hash. The input may optionally
// be prefixed by 0x and can have a byte length up to 32.
func decodeHash(s string) (h common.Hash, inputLength int, err error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	if (len(s) & 1) > 0 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("invalid hex string: %s", s)
	}
	if len(b) > common.HashLength {
		return common.Hash{}, len(b), fmt.Errorf(
			"hex string too long, want at most 32 bytes, have %d bytes",
			len(b),
		)
	}
	return common.BytesToHash(b), len(b), nil
}

/*
Static responses section

The API endpoints bellow return a static response because the values are not relevant for Flow EVM implementation
or because it doesn't make sense yet to implement more complex solution
*/

// ChainId is the EIP-155 replay-protection chain id for the current Ethereum chain config.
//
// Note, this method does not conform to EIP-695 because the configured chain ID is always
// returned, regardless of the current head block. We used to return an error when the chain
// wasn't synced up to a block where EIP-155 is enabled, but this behavior caused issues
// in CL clients.
func (b *BlockChainAPI) ChainId(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(b.config.EVMNetworkID), nil
}

// Coinbase is the address that mining rewards will be sent to (alias for Etherbase).
func (b *BlockChainAPI) Coinbase(ctx context.Context) (common.Address, error) {
	return b.config.Coinbase, nil
}

// GasPrice returns a suggestion for a gas price for legacy transactions.
func (b *BlockChainAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(b.config.GasPrice), nil
}

// GetUncleCountByBlockHash returns number of uncles in the block for the given block hash
func (b *BlockChainAPI) GetUncleCountByBlockHash(
	ctx context.Context,
	blockHash common.Hash,
) *hexutil.Uint {
	count := hexutil.Uint(0)
	return &count
}

// GetUncleCountByBlockNumber returns number of uncles in the block for the given block number
func (b *BlockChainAPI) GetUncleCountByBlockNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
) *hexutil.Uint {
	count := hexutil.Uint(0)
	return &count
}

// GetUncleByBlockHashAndIndex returns the uncle block for the given block hash and index.
func (b *BlockChainAPI) GetUncleByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// GetUncleByBlockNumberAndIndex returns the uncle block for the given block hash and index.
func (b *BlockChainAPI) GetUncleByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// MaxPriorityFeePerGas returns a suggestion for a gas tip cap for dynamic fee transactions.
func (b *BlockChainAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	fee := hexutil.Big(*big.NewInt(1))
	return &fee, nil
}

// Mining returns true if client is actively mining new blocks.
// This can only return true for proof-of-work networks and may
// not be available in some clients since The Merge.
func (b *BlockChainAPI) Mining() bool {
	return false
}

// Hashrate returns the number of hashes per second that the
// node is mining with.
// This can only return true for proof-of-work networks and
// may not be available in some clients since The Merge.
func (b *BlockChainAPI) Hashrate() hexutil.Uint64 {
	return hexutil.Uint64(0)
}

/*
Not supported section

The API endpoints bellow return a non-supported error indicating the API requested is not supported (yet).
This is because a decision to not support this API was made either because we don't intend to support it
ever or we don't support it at this phase.
*/

// GetProof returns the Merkle-proof for a given account and optionally some storage keys.
func (b *BlockChainAPI) GetProof(
	ctx context.Context,
	address common.Address,
	storageKeys []string,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*AccountResult, error) {
	return nil, errs.NewEndpointNotSupportedError("eth_getProof")
}

// CreateAccessList creates an EIP-2930 type AccessList for the given transaction.
// Reexec and blockNumberOrHash can be specified to create the accessList on top of a certain state.
func (b *BlockChainAPI) CreateAccessList(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*AccessListResult, error) {
	return nil, errs.NewEndpointNotSupportedError("eth_createAccessList")
}
