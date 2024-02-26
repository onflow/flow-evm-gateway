package api

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	errs "github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/services/requester"
	"github.com/onflow/flow-evm-gateway/storage"
	storageErrs "github.com/onflow/flow-evm-gateway/storage/errors"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
)

func SupportedAPIs(blockChainAPI *BlockChainAPI) []rpc.API {
	return []rpc.API{{
		Namespace: "eth",
		Service:   blockChainAPI,
	}, {
		Namespace: "web3",
		Service:   &Web3API{},
	}, {
		Namespace: "net",
		Service:   &NetAPI{},
	}}
}

type BlockChainAPI struct {
	logger       zerolog.Logger
	config       *config.Config
	evm          requester.Requester
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	accounts     storage.AccountIndexer
}

func NewBlockChainAPI(
	logger zerolog.Logger,
	config *config.Config,
	evm requester.Requester,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
) *BlockChainAPI {
	return &BlockChainAPI{
		logger:       logger,
		config:       config,
		evm:          evm,
		blocks:       blocks,
		transactions: transactions,
		receipts:     receipts,
		accounts:     accounts,
	}
}

// ChainId is the EIP-155 replay-protection chain id for the current Ethereum chain config.
//
// Note, this method does not conform to EIP-695 because the configured chain ID is always
// returned, regardless of the current head block. We used to return an error when the chain
// wasn't synced up to a block where EIP-155 is enabled, but this behavior caused issues
// in CL clients.
func (b *BlockChainAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(b.config.EVMNetworkID)
}

// BlockNumber returns the block number of the chain head.
func (b *BlockChainAPI) BlockNumber() (hexutil.Uint64, error) {
	latestBlockHeight, err := b.blocks.LatestEVMHeight()
	if err != nil {
		return handleError[hexutil.Uint64](b.logger, err)
	}

	return hexutil.Uint64(latestBlockHeight), nil
}

// Syncing returns false in case the node is currently not syncing with the network. It can be up-to-date or has not
// yet received the latest block headers from its pears. In case it is synchronizing:
// - startingBlock: block number this node started to synchronize from
// - currentBlock:  block number this node is currently importing
// - highestBlock:  block number of the highest block header this node has received from peers
// - pulledStates:  number of state entries processed until now
// - knownStates:   number of known state entries that still need to be pulled
func (b *BlockChainAPI) Syncing() (interface{}, error) {
	// todo maybe we should check if the node is caught up with the latest flow block
	return false, nil
}

// SendRawTransaction will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce.
func (b *BlockChainAPI) SendRawTransaction(
	ctx context.Context,
	input hexutil.Bytes,
) (common.Hash, error) {
	id, err := b.evm.SendRawTransaction(ctx, input)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to send raw transaction")
		return common.Hash{}, errs.ErrInternal
	}

	return id, nil
}

// GasPrice returns a suggestion for a gas price for legacy transactions.
func (b *BlockChainAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(b.config.GasPrice), nil
}

// GetBalance returns the amount of wei for the given address in the state of the
// given block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta
// block numbers are also allowed.
func (b *BlockChainAPI) GetBalance(
	ctx context.Context,
	address common.Address,
	_ *rpc.BlockNumberOrHash,
) (*hexutil.Big, error) {
	// todo use provided height, when height mapping is implemented
	balance, err := b.evm.GetBalance(ctx, address, 0)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to get balance")
		return nil, errs.ErrInternal
	}

	return (*hexutil.Big)(balance), nil
}

// GetTransactionByHash returns the transaction for the given hash
func (b *BlockChainAPI) GetTransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*RPCTransaction, error) {
	tx, err := b.transactions.Get(hash)
	if err != nil {
		return handleError[*RPCTransaction](b.logger, err)
	}

	rcp, err := b.receipts.GetByTransactionID(tx.Hash())
	if err != nil {
		return handleError[*RPCTransaction](b.logger, err)
	}

	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to calculate sender")
		return nil, errs.ErrInternal
	}

	v, r, s := tx.RawSignatureValues()
	index := uint64(rcp.TransactionIndex)

	txResult := &RPCTransaction{
		Hash:             tx.Hash(),
		BlockHash:        &rcp.BlockHash,
		BlockNumber:      (*hexutil.Big)(rcp.BlockNumber),
		From:             from,
		To:               tx.To(),
		Gas:              hexutil.Uint64(rcp.GasUsed),
		GasPrice:         (*hexutil.Big)(rcp.EffectiveGasPrice),
		Input:            tx.Data(),
		Nonce:            hexutil.Uint64(tx.Nonce()),
		TransactionIndex: (*hexutil.Uint64)(&index),
		Value:            (*hexutil.Big)(tx.Value()),
		Type:             hexutil.Uint64(tx.Type()),
		V:                (*hexutil.Big)(v),
		R:                (*hexutil.Big)(r),
		S:                (*hexutil.Big)(s),
	}
	return txResult, nil
}

// GetTransactionByBlockHashAndIndex returns the transaction for the given block hash and index.
func (b *BlockChainAPI) GetTransactionByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) (*RPCTransaction, error) { // todo add error to the return type
	block, err := b.blocks.GetByID(blockHash)
	if err != nil {
		return handleError[*RPCTransaction](b.logger, err)
	}

	highestIndex := len(block.TransactionHashes) - 1
	if index > hexutil.Uint(highestIndex) {
		return nil, nil
	}

	txHash := block.TransactionHashes[index]
	tx, err := b.GetTransactionByHash(ctx, txHash)
	if err != nil {
		return handleError[*RPCTransaction](b.logger, err)
	}

	return tx, nil
}

// GetTransactionByBlockNumberAndIndex returns the transaction for the given block number and index.
func (b *BlockChainAPI) GetTransactionByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) (*RPCTransaction, error) { // todo add error to the return type
	block, err := b.blocks.GetByHeight(uint64(blockNumber))
	if err != nil {
		return handleError[*RPCTransaction](b.logger, err)
	}

	highestIndex := len(block.TransactionHashes) - 1
	if index > hexutil.Uint(highestIndex) {
		return nil, nil
	}

	txHash := block.TransactionHashes[index]
	tx, err := b.GetTransactionByHash(ctx, txHash)
	if err != nil {
		return handleError[*RPCTransaction](b.logger, err)
	}

	return tx, nil
}

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (b *BlockChainAPI) GetTransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (*types.Receipt, error) {
	_, err := b.transactions.Get(hash)
	if err != nil {
		return handleError[*types.Receipt](b.logger, err)
	}

	rcp, err := b.receipts.GetByTransactionID(hash)
	if err != nil {
		return handleError[*types.Receipt](b.logger, err)
	}

	// todo workaround until new version of flow-go is released
	blk, _ := b.blocks.GetByHeight(rcp.BlockNumber.Uint64())
	h, _ := blk.Hash()
	rcp.BlockHash = h

	return rcp, nil
}

// Coinbase is the address that mining rewards will be sent to (alias for Etherbase).
func (b *BlockChainAPI) Coinbase() (common.Address, error) {
	return b.config.Coinbase, nil
}

// GetBlockByHash returns the requested block. When fullTx is true all transactions in the block are returned in full
// detail, otherwise only the transaction hash is returned.
func (b *BlockChainAPI) GetBlockByHash(
	ctx context.Context,
	hash common.Hash,
	fullTx bool,
) (*Block, error) {
	bl, err := b.blocks.GetByID(hash)
	if err != nil {
		return handleError[*Block](b.logger, err)
	}

	h, err := bl.Hash()
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to calculate hash for block by hash")
		return nil, errs.ErrInternal
	}

	return &Block{
		Hash:         h,
		Number:       hexutil.Uint64(bl.Height),
		ParentHash:   bl.ParentBlockHash,
		ReceiptsRoot: bl.ReceiptRoot,
		Transactions: bl.TransactionHashes,
	}, nil
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
	if fullTx { // todo support full tx
		b.logger.Warn().Msg("not supported getting full txs")
	}

	height := uint64(blockNumber)
	var err error
	// todo for now we treat all special blocks as latest, think which special statuses we can even support on Flow
	if blockNumber < 0 {
		height, err = b.blocks.LatestEVMHeight()
		if err != nil {
			return handleError[*Block](b.logger, err)
		}
	}

	bl, err := b.blocks.GetByHeight(height)
	if err != nil {
		return handleError[*Block](b.logger, err)
	}

	h, err := bl.Hash()
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to calculate hash for block by number")
		return nil, errs.ErrInternal
	}

	return &Block{
		Hash:         h,
		Number:       hexutil.Uint64(bl.Height),
		ParentHash:   bl.ParentBlockHash,
		ReceiptsRoot: bl.ReceiptRoot,
		Transactions: bl.TransactionHashes,
	}, nil
}

// GetBlockReceipts returns the block receipts for the given block hash or number or tag.
func (b *BlockChainAPI) GetBlockReceipts(
	ctx context.Context,
	numHash rpc.BlockNumberOrHash,
) ([]*types.Receipt, error) { // todo validate return type
	var (
		block *evmTypes.Block
		err   error
	)
	if numHash.BlockHash != nil {
		block, err = b.blocks.GetByID(*numHash.BlockHash)
	} else if numHash.BlockNumber != nil {
		block, err = b.blocks.GetByHeight(uint64(numHash.BlockNumber.Int64()))
	} else {
		return nil, errors.Join(
			errs.ErrInvalid,
			fmt.Errorf("block number or hash not provided"),
		)
	}
	if err != nil {
		return handleError[[]*types.Receipt](b.logger, err)
	}

	receipts := make([]*types.Receipt, len(block.TransactionHashes))
	for i, hash := range block.TransactionHashes {
		rcp, err := b.receipts.GetByTransactionID(hash)
		if err != nil {
			return handleError[[]*types.Receipt](b.logger, err)
		}
		receipts[i] = rcp
	}

	return receipts, nil
}

// GetBlockTransactionCountByHash returns the number of transactions in the block with the given hash.
func (b *BlockChainAPI) GetBlockTransactionCountByHash(
	ctx context.Context,
	blockHash common.Hash,
) (*hexutil.Uint, error) {
	block, err := b.blocks.GetByID(blockHash)
	if err != nil {
		return handleError[*hexutil.Uint](b.logger, err)
	}

	count := hexutil.Uint(len(block.TransactionHashes))
	return &count, nil
}

// GetBlockTransactionCountByNumber returns the number of transactions in the block with the given block number.
func (b *BlockChainAPI) GetBlockTransactionCountByNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
) (*hexutil.Uint, error) {
	if blockNumber < rpc.EarliestBlockNumber {
		// todo handle block number for negative special values in all APIs
		return nil, errs.ErrNotSupported
	}

	block, err := b.blocks.GetByHeight(uint64(blockNumber))
	if err != nil {
		return handleError[*hexutil.Uint](b.logger, err)
	}

	count := hexutil.Uint(len(block.TransactionHashes))
	return &count, nil
}

// Call executes the given transaction on the state for the given block number.
//
// Additionally, the caller can specify a batch of contract for fields overriding.
//
// Note, this function doesn't make and changes in the state/blockchain and is
// useful to execute and retrieve values.
func (b *BlockChainAPI) Call(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
	overrides *StateOverride,
	blockOverrides *BlockOverrides,
) (hexutil.Bytes, error) {
	if args.To == nil {
		// todo temporary fix, support empty to address
		return nil, nil
	}

	var data []byte
	if args.Data != nil {
		data = *args.Data
	} else if args.Input != nil {
		data = *args.Input
	} else {
		return nil, errors.Join(
			errs.ErrInvalid,
			fmt.Errorf("must provide either data or input values: %w", errs.ErrInvalid),
		)
	}

	res, err := b.evm.Call(ctx, *args.To, data)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to execute call")
		return nil, errs.ErrInternal
	}

	return res, nil
}

// GetLogs returns logs matching the given argument that are stored within the state.
func (b *BlockChainAPI) GetLogs(
	ctx context.Context,
	criteria filters.FilterCriteria,
) ([]*types.Log, error) {

	filter := logs.FilterCriteria{
		Addresses: criteria.Addresses,
		Topics:    criteria.Topics,
	}

	if criteria.BlockHash != nil {
		return logs.
			NewIDFilter(*criteria.BlockHash, filter, b.blocks, b.receipts).
			Match()
	}
	if criteria.FromBlock != nil && criteria.ToBlock != nil {
		return logs.
			NewRangeFilter(*criteria.FromBlock, *criteria.ToBlock, filter, b.receipts).
			Match()
	}

	return nil, errors.Join(
		errs.ErrInvalid,
		fmt.Errorf("must provide either block ID or 'from' and 'to' block nubmers, to filter events"),
	)
}

// GetTransactionCount returns the number of transactions the given address has sent for the given block number
func (b *BlockChainAPI) GetTransactionCount(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*hexutil.Uint64, error) {
	// todo support previous nonce
	if blockNumberOrHash != nil && *blockNumberOrHash.BlockNumber != rpc.LatestBlockNumber {
		b.logger.Warn().Msg("transaction count for special blocks not supported") // but still return latest for now
	}

	nonce, err := b.accounts.GetNonce(&address)
	if err != nil {
		b.logger.Error().Err(err).Msg("get nonce failed")
		return nil, errs.ErrInternal
	}

	return (*hexutil.Uint64)(&nonce), nil
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
	var data []byte
	if args.Data != nil {
		data = *args.Data
	} else if args.Input != nil {
		data = *args.Input
	}

	eoa := newEOATestAccount(b.config.COAKey.String()[2:])
	txData := eoa.PrepareSignAndEncodeTx(
		args.To,
		data,
		(*big.Int)(args.Value),
		uint64(*args.Gas),
		(*big.Int)(args.GasPrice),
	)

	gas, err := b.evm.EstimateGas(ctx, txData)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to estimate gas")
		return hexutil.Uint64(0), err
	}

	return hexutil.Uint64(gas), nil
}

func handleError[T any](log zerolog.Logger, err error) (T, error) {
	var zero T
	if errors.Is(err, storageErrs.NotFound) {
		// as per specification returning nil and nil for not found resources
		return zero, nil
	}

	log.Error().Err(err).Msg("failed to get latest block height")
	return zero, errs.ErrInternal
}

/* ====================================================================================================================

 NOT SUPPORTED SECTION

====================================================================================================================== */
// todo check the design, maybe we don't need to even have the unsupported functions defined

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

// NewFilter creates a new filter and returns the filter id. It can be
// used to retrieve logs when the state changes. This method cannot be
// used to fetch logs that are already stored in the state.
//
// Default criteria for the from and to block are "latest".
// Using "latest" as block number will return logs for mined blocks.
// Using "pending" as block number returns logs for not yet mined (pending) blocks.
// In case logs are removed (chain reorg) previously returned logs are returned
// again but with the removed property set to true.
//
// In case "fromBlock" > "toBlock" an error is returned.
func (b *BlockChainAPI) NewFilter(
	criteria filters.FilterCriteria,
) (rpc.ID, error) {
	return "", errs.ErrNotSupported
}

// UninstallFilter removes the filter with the given filter id.
func (b *BlockChainAPI) UninstallFilter(id rpc.ID) bool {
	return false
}

// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found an empty array of logs is returned.
func (b *BlockChainAPI) GetFilterLogs(
	ctx context.Context,
	id rpc.ID,
) ([]*types.Log, error) {
	return nil, errs.ErrNotSupported
}

// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (b *BlockChainAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	return nil, errs.ErrNotSupported
}

// NewBlockFilter creates a filter that fetches blocks that are imported into the chain.
// It is part of the filter package since polling goes with eth_getFilterChanges.
func (b *BlockChainAPI) NewBlockFilter() rpc.ID {
	return ""
}

// NewPendingTransactionFilter creates a filter that fetches pending transactions
// as transactions enter the pending state.
//
// It is part of the filter package because this filter can be used through the
// `eth_getFilterChanges` polling method that is also used for log filters.
func (b *BlockChainAPI) NewPendingTransactionFilter(fullTx *bool) rpc.ID {
	return ""
}

// Accounts returns the collection of accounts this node manages.
func (b *BlockChainAPI) Accounts() []common.Address {
	return nil
}

// GetUncleByBlockHashAndIndex returns the uncle block for the given block hash and index.
func (b *BlockChainAPI) GetUncleByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return nil, errs.ErrNotSupported
}

// GetUncleByBlockNumberAndIndex returns the uncle block for the given block hash and index.
func (b *BlockChainAPI) GetUncleByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return nil, errs.ErrNotSupported
}

// Sign calculates an ECDSA signature for:
// keccak256("\x19Ethereum Signed Message:\n" + len(message) + message).
//
// Note, the produced signature conforms to the secp256k1 curve R, S and V values,
// where the V value will be 27 or 28 for legacy reasons.
//
// The account associated with addr must be unlocked.
//
// https://github.com/ethereum/wiki/wiki/JSON-RPC#eth_sign
func (b *BlockChainAPI) Sign(
	addr common.Address,
	data hexutil.Bytes,
) (hexutil.Bytes, error) {
	return nil, errs.ErrNotSupported
}

// SignTransaction will sign the given transaction with the from account.
// The node needs to have the private key of the account corresponding with
// the given from address and it needs to be unlocked.
func (b *BlockChainAPI) SignTransaction(
	ctx context.Context,
	args TransactionArgs,
) (*SignTransactionResult, error) {
	return nil, errs.ErrNotSupported
}

// SendTransaction creates a transaction for the given argument, sign it
// and submit it to the transaction pool.
func (b *BlockChainAPI) SendTransaction(
	ctx context.Context,
	args TransactionArgs,
) (common.Hash, error) {
	return common.Hash{}, errs.ErrNotSupported
}

// GetCode returns the code stored at the given address in the state for the given block number.
func (b *BlockChainAPI) GetCode(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	return nil, errs.ErrNotSupported
}

// GetProof returns the Merkle-proof for a given account and optionally some storage keys.
func (b *BlockChainAPI) GetProof(
	ctx context.Context,
	address common.Address,
	storageKeys []string,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*AccountResult, error) {
	return nil, errs.ErrNotSupported
}

// GetStorageAt returns the storage from the state at the given address, key and
// block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta block
// numbers are also allowed.
func (b *BlockChainAPI) GetStorageAt(
	ctx context.Context,
	address common.Address,
	storageSlot string,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	return nil, errs.ErrNotSupported
}

// CreateAccessList creates an EIP-2930 type AccessList for the given transaction.
// Reexec and blockNumberOrHash can be specified to create the accessList on top of a certain state.
func (b *BlockChainAPI) CreateAccessList(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*AccessListResult, error) {
	return nil, errs.ErrNotSupported
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
	return nil, errs.ErrNotSupported
}

// MaxPriorityFeePerGas returns a suggestion for a gas tip cap for dynamic fee transactions.
func (b *BlockChainAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	return nil, errs.ErrNotSupported
}
