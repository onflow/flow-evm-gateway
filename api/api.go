package api

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
)

type BlockChainAPI struct{}

// eth_chainId
// ChainId is the EIP-155 replay-protection chain id for the current Ethereum chain config.
//
// Note, this method does not conform to EIP-695 because the configured chain ID is always
// returned, regardless of the current head block. We used to return an error when the chain
// wasn't synced up to a block where EIP-155 is enabled, but this behavior caused issues
// in CL clients.
func (api *BlockChainAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(big.NewInt(777))
}

// eth_blockNumber
// BlockNumber returns the block number of the chain head.
func (api *BlockChainAPI) BlockNumber() hexutil.Uint64 {
	return hexutil.Uint64(65848272)
}

// eth_syncing
// Syncing returns false in case the node is currently not syncing with the network. It can be up-to-date or has not
// yet received the latest block headers from its pears. In case it is synchronizing:
// - startingBlock: block number this node started to synchronize from
// - currentBlock:  block number this node is currently importing
// - highestBlock:  block number of the highest block header this node has received from peers
// - pulledStates:  number of state entries processed until now
// - knownStates:   number of known state entries that still need to be pulled
func (api *BlockChainAPI) Syncing() (interface{}, error) {
	return false, nil
}

// eth_sendRawTransaction
// SendRawTransaction will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce.
func (api *BlockChainAPI) SendRawTransaction(
	ctx context.Context,
	input []byte,
) (common.Hash, error) {
	return common.Hash{}, nil
}

// eth_createAccessList
// CreateAccessList creates an EIP-2930 type AccessList for the given transaction.
// Reexec and blockNumberOrHash can be specified to create the accessList on top of a certain state.
func (s *BlockChainAPI) CreateAccessList(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*accessListResult, error) {
	return &accessListResult{GasUsed: hexutil.Uint64(105)}, nil
}

// eth_feeHistory (transaction fee history)
// FeeHistory returns transaction base fee per gas and effective priority fee
// per gas for the requested/supported block range.
// blockCount: Requested range of blocks. Clients will return less than the
// requested range if not all blocks are available.
// lastBlock: Highest block of the requested range.
// rewardPercentiles: A monotonically increasing list of percentile values.
// For each block in the requested range, the transactions will be sorted in
// ascending order by effective tip per gas and the coresponding effective tip
// for the percentile will be determined, accounting for gas consumed.
func (s *BlockChainAPI) FeeHistory(
	ctx context.Context,
	blockCount math.HexOrDecimal64,
	lastBlock rpc.BlockNumber,
	rewardPercentiles []float64,
) (*feeHistoryResult, error) {
	results := &feeHistoryResult{
		OldestBlock:  (*hexutil.Big)(big.NewInt(10102020506)),
		GasUsedRatio: []float64{105.0},
	}
	return results, nil
}

// eth_gasPrice (returns the gas price)
// GasPrice returns a suggestion for a gas price for legacy transactions.
func (s *BlockChainAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(10102020506)), nil
}

// eth_maxPriorityFeePerGas
// MaxPriorityFeePerGas returns a suggestion for a gas tip cap for dynamic fee transactions.
func (s *BlockChainAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(10102020506)), nil
}

// eth_getBalance (returns the balance for any given block)
// GetBalance returns the amount of wei for the given address in the state of the
// given block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta
// block numbers are also allowed.
func (s *BlockChainAPI) GetBalance(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(101)), nil
}

// eth_getCode (returns the code for the given address)
// GetCode returns the code stored at the given address in the state for the given block number.
func (s *BlockChainAPI) GetCode(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	return hexutil.Bytes{}, nil
}

// eth_getProof (returns state proof for an account)
// GetProof returns the Merkle-proof for a given account and optionally some storage keys.
func (s *BlockChainAPI) GetProof(
	ctx context.Context,
	address common.Address,
	storageKeys []string,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*AccountResult, error) {
	storageProof := make([]StorageResult, len(storageKeys))
	return &AccountResult{
		Address:      address,
		AccountProof: []string{""},
		Balance:      (*hexutil.Big)(big.NewInt(10011)),
		CodeHash:     common.Hash{},
		Nonce:        hexutil.Uint64(10502),
		StorageHash:  common.Hash{},
		StorageProof: storageProof,
	}, nil
}

// eth_getStorageAt (we probably won't support this day 1)
// GetStorageAt returns the storage from the state at the given address, key and
// block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta block
// numbers are also allowed.
func (s *BlockChainAPI) GetStorageAt(
	ctx context.Context,
	address common.Address,
	hexKey string,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	return hexutil.Bytes{}, nil
}

// eth_getTransactionCount (returns the number of tx sent from an address (nonce))
// GetTransactionCount returns the number of transactions the given address has sent for the given block number
func (s *BlockChainAPI) GetTransactionCount(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*hexutil.Uint64, error) {
	nonce := uint64(1050510)
	return (*hexutil.Uint64)(&nonce), nil
}

// eth_getTransactionByHash
// GetTransactionByHash returns the transaction for the given hash
func (s *BlockChainAPI) GetTransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*RPCTransaction, error) {
	return &RPCTransaction{}, nil
}

// eth_getTransactionByBlockHashAndIndex
// GetTransactionByBlockHashAndIndex returns the transaction for the given block hash and index.
func (s *BlockChainAPI) GetTransactionByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) *RPCTransaction {
	return &RPCTransaction{}
}

// eth_getTransactionByBlockNumberAndIndex
// GetTransactionByBlockNumberAndIndex returns the transaction for the given block number and index.
func (s *BlockChainAPI) GetTransactionByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) *RPCTransaction {
	return &RPCTransaction{}
}

// eth_getTransactionReceipt
// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (s *BlockChainAPI) GetTransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// eth_coinbase (return the coinbase for a block)
// Coinbase is the address that mining rewards will be sent to (alias for Etherbase).
func (s *BlockChainAPI) Coinbase() (common.Address, error) {
	return common.Address{}, nil
}

// eth_getBlockByHash
// GetBlockByHash returns the requested block. When fullTx is true all transactions in the block are returned in full
// detail, otherwise only the transaction hash is returned.
func (s *BlockChainAPI) GetBlockByHash(
	ctx context.Context,
	hash common.Hash,
	fullTx bool,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// eth_getBlockByNumber
// GetBlockByNumber returns the requested canonical block.
//   - When blockNr is -1 the chain pending block is returned.
//   - When blockNr is -2 the chain latest block is returned.
//   - When blockNr is -3 the chain finalized block is returned.
//   - When blockNr is -4 the chain safe block is returned.
//   - When fullTx is true all transactions in the block are returned, otherwise
//     only the transaction hash is returned.
func (s *BlockChainAPI) GetBlockByNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	fullTx bool,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// eth_getBlockReceipts
// GetBlockReceipts returns the block receipts for the given block hash or number or tag.
func (s *BlockChainAPI) GetBlockReceipts(
	ctx context.Context,
	blockNumberOrHash rpc.BlockNumberOrHash,
) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0)
	return result, nil
}

// eth_getBlockTransactionCountByHash
// GetBlockTransactionCountByHash returns the number of transactions in the block with the given hash.
func (s *BlockChainAPI) GetBlockTransactionCountByHash(
	ctx context.Context,
	blockHash common.Hash,
) *hexutil.Uint {
	count := hexutil.Uint(100522)
	return &count
}

// eth_getBlockTransactionCountByNumber
// GetBlockTransactionCountByNumber returns the number of transactions in the block with the given block number.
func (s *BlockChainAPI) GetBlockTransactionCountByNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
) *hexutil.Uint {
	count := hexutil.Uint(522)
	return &count
}

// eth_getUncleCountByBlockHash (return empty)
// GetUncleCountByBlockHash returns number of uncles in the block for the given block hash
func (s *BlockChainAPI) GetUncleCountByBlockHash(
	ctx context.Context,
	blockHash common.Hash,
) *hexutil.Uint {
	count := hexutil.Uint(0)
	return &count
}

// eth_getUncleCountByBlockNumber (return empty)
// GetUncleCountByBlockNumber returns number of uncles in the block for the given block number
func (s *BlockChainAPI) GetUncleCountByBlockNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
) *hexutil.Uint {
	count := hexutil.Uint(0)
	return &count
}

// eth_getLogs
// GetLogs returns logs matching the given argument that are stored within the state.
func (s *BlockChainAPI) GetLogs(
	ctx context.Context,
	criteria filters.FilterCriteria,
) ([]*types.Log, error) {
	if len(criteria.Topics) > maxTopics {
		return nil, errExceedMaxTopics
	}

	return []*types.Log{}, nil
}

// eth_newFilter
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
func (s *BlockChainAPI) NewFilter(
	criteria filters.FilterCriteria,
) (rpc.ID, error) {
	return "", nil
}

// eth_uninstallFilter
// UninstallFilter removes the filter with the given filter id.
func (s *BlockChainAPI) UninstallFilter(id rpc.ID) bool {
	return false
}

// eth_getFilterLogs
// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found an empty array of logs is returned.
func (s *BlockChainAPI) GetFilterLogs(
	ctx context.Context,
	id rpc.ID,
) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

// eth_getFilterChanges
// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (s *BlockChainAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	return []interface{}{}, errFilterNotFound
}

// eth_newBlockFilter
// NewBlockFilter creates a filter that fetches blocks that are imported into the chain.
// It is part of the filter package since polling goes with eth_getFilterChanges.
func (s *BlockChainAPI) NewBlockFilter() rpc.ID {
	return ""
}

// eth_newPendingTransactionFilter
// NewPendingTransactionFilter creates a filter that fetches pending transactions
// as transactions enter the pending state.
//
// It is part of the filter package because this filter can be used through the
// `eth_getFilterChanges` polling method that is also used for log filters.
func (s *BlockChainAPI) NewPendingTransactionFilter(fullTx *bool) rpc.ID {
	return ""
}

// eth_accounts
// Accounts returns the collection of accounts this node manages.
func (s *BlockChainAPI) Accounts() []common.Address {
	return []common.Address{}
}

// eth_sign
// Sign calculates an Ethereum ECDSA signature for:
// keccak256("\x19Ethereum Signed Message:\n" + len(message) + message))
//
// Note, the produced signature conforms to the secp256k1 curve R, S and V values,
// where the V value will be 27 or 28 for legacy reasons.
//
// The key used to calculate the signature is decrypted with the given password.
//
// https://github.com/ethereum/go-ethereum/wiki/Management-APIs#personal_sign
func (s *BlockChainAPI) Sign(
	ctx context.Context,
	data hexutil.Bytes,
	address common.Address,
	password string,
) (hexutil.Bytes, error) {
	return hexutil.Bytes{}, fmt.Errorf("not implemented")
}

// eth_signTransaction
// SignTransaction will sign the given transaction with the from account.
// The node needs to have the private key of the account corresponding with
// the given from address and it needs to be unlocked.
func (s *BlockChainAPI) SignTransaction(
	ctx context.Context,
	args TransactionArgs,
) (*SignTransactionResult, error) {
	return &SignTransactionResult{}, fmt.Errorf("not implemented")
}

// eth_sendTransaction
// SendTransaction will create a transaction from the given arguments and
// tries to sign it with the key associated with args.From. If the given
// passwd isn't able to decrypt the key it fails.
func (s *BlockChainAPI) SendTransaction(
	ctx context.Context,
	args TransactionArgs,
	password string,
) (common.Hash, error) {
	return common.Hash{}, fmt.Errorf("not implemented")
}

// eth_call (readonly calls, we might need this (if the wallet use it to get balanceOf an ERC-20))
// Call executes the given transaction on the state for the given block number.
//
// Additionally, the caller can specify a batch of contract for fields overriding.
//
// Note, this function doesn't make and changes in the state/blockchain and is
// useful to execute and retrieve values.
func (s *BlockChainAPI) Call(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
	overrides *StateOverride,
	blockOverrides *BlockOverrides,
) (hexutil.Bytes, error) {
	return hexutil.Bytes{}, nil
}

// eth_estimateGas (usually runs the call and checks how much gas might be used)
// EstimateGas returns the lowest possible gas limit that allows the transaction to run
// successfully at block `blockNrOrHash`, or the latest block if `blockNrOrHash` is unspecified. It
// returns error if the transaction would revert or if there are unexpected failures. The returned
// value is capped by both `args.Gas` (if non-nil & non-zero) and the backend's RPCGasCap
// configuration (if non-zero).
func (s *BlockChainAPI) EstimateGas(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
	overrides *StateOverride,
) (hexutil.Uint64, error) {
	return hexutil.Uint64(105), nil
}
