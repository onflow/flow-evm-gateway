package api

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/crypto"
)

const ethNamespace = "eth"

//go:embed cadence/scripts/bridged_account_call.cdc
var BridgedAccountCall []byte

//go:embed cadence/transactions/evm_run.cdc
var EVMRunTx []byte

//go:embed cadence/scripts/evm_address_balance.cdc
var EVMAddressBalance []byte

func SupportedAPIs(blockChainAPI *BlockChainAPI) []rpc.API {
	return []rpc.API{
		{
			Namespace: ethNamespace,
			Service:   blockChainAPI,
		},
	}
}

type BlockChainAPI struct {
	config       *config.Config
	Store        *storage.Store
	FlowClient   access.Client
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipt      storage.ReceiptIndexer
}

func NewBlockChainAPI(
	config *config.Config,
	store *storage.Store,
	flowClient access.Client,
) *BlockChainAPI {
	return &BlockChainAPI{
		config:     config,
		Store:      store,
		FlowClient: flowClient,
	}
}

// ChainId is the EIP-155 replay-protection chain id for the current Ethereum chain config.
//
// Note, this method does not conform to EIP-695 because the configured chain ID is always
// returned, regardless of the current head block. We used to return an error when the chain
// wasn't synced up to a block where EIP-155 is enabled, but this behavior caused issues
// in CL clients.
func (b *BlockChainAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(b.config.ChainID)
}

// BlockNumber returns the block number of the chain head.
func (b *BlockChainAPI) BlockNumber() hexutil.Uint64 {
	latestBlockHeight, err := b.blocks.LatestHeight()
	if err != nil {
		// TODO(m-Peter) We should add a logger to BlockChainAPI
		panic(fmt.Errorf("failed to fetch the latest block number: %v", err))
	}
	return hexutil.Uint64(latestBlockHeight)
}

// Syncing returns false in case the node is currently not syncing with the network. It can be up-to-date or has not
// yet received the latest block headers from its pears. In case it is synchronizing:
// - startingBlock: block number this node started to synchronize from
// - currentBlock:  block number this node is currently importing
// - highestBlock:  block number of the highest block header this node has received from peers
// - pulledStates:  number of state entries processed until now
// - knownStates:   number of known state entries that still need to be pulled
func (b *BlockChainAPI) Syncing() (interface{}, error) {
	return false, nil
}

// SendRawTransaction will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce.
func (b *BlockChainAPI) SendRawTransaction(
	ctx context.Context,
	input hexutil.Bytes,
) (common.Hash, error) {
	gethTx, err := GethTxFromBytes(input)
	if err != nil {
		return common.Hash{}, err
	}

	latestBlock, err := b.FlowClient.GetLatestBlock(ctx, true)
	if err != nil {
		return common.Hash{}, err
	}

	// TODO(m-Peter): Fetch the private key from a dedicated flow.json config file
	privateKey, err := crypto.DecodePrivateKeyHex(
		crypto.ECDSA_P256,
		"2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21",
	)
	if err != nil {
		return common.Hash{}, err
	}

	// TODO(m-Peter): Fetch the address from a dedicated flow.json config file
	account, err := b.FlowClient.GetAccount(ctx, flow.HexToAddress("0xf8d6e0586b0a20c7"))
	if err != nil {
		return common.Hash{}, err
	}
	accountKey := account.Keys[0]
	signer, err := crypto.NewInMemorySigner(privateKey, accountKey.HashAlgo)
	if err != nil {
		return common.Hash{}, err
	}

	tx := flow.NewTransaction().
		SetScript(EVMRunTx).
		SetProposalKey(account.Address, accountKey.Index, accountKey.SequenceNumber).
		SetReferenceBlockID(latestBlock.ID).
		SetPayer(account.Address).
		AddAuthorizer(account.Address)

	txArgument := CadenceByteArrayFromBytes(input)
	tx.AddArgument(txArgument)

	err = tx.SignEnvelope(account.Address, accountKey.Index, signer)
	if err != nil {
		return common.Hash{}, err
	}

	err = b.FlowClient.SendTransaction(ctx, *tx)
	if err != nil {
		return common.Hash{}, err
	}

	return gethTx.Hash(), nil
}

// CreateAccessList creates an EIP-2930 type AccessList for the given transaction.
// Reexec and blockNumberOrHash can be specified to create the accessList on top of a certain state.
func (b *BlockChainAPI) CreateAccessList(
	ctx context.Context,
	args TransactionArgs,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*AccessListResult, error) {
	return nil, errors.NotSupported
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
	return nil, errors.NotSupported
}

// GasPrice returns a suggestion for a gas price for legacy transactions.
func (b *BlockChainAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(b.config.GasPrice), nil
}

// MaxPriorityFeePerGas returns a suggestion for a gas tip cap for dynamic fee transactions.
func (b *BlockChainAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	return nil, errors.NotSupported
}

// GetBalance returns the amount of wei for the given address in the state of the
// given block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta
// block numbers are also allowed.
func (b *BlockChainAPI) GetBalance(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*hexutil.Big, error) {
	evmAddress := CadenceEVMAddressFromBytes(address.Bytes())
	value, err := b.FlowClient.ExecuteScriptAtLatestBlock(
		ctx,
		EVMAddressBalance,
		[]cadence.Value{evmAddress},
	)
	if err != nil {
		return nil, err
	}

	balance, ok := value.(cadence.UInt)
	if !ok {
		return nil, fmt.Errorf("script doesn't return UInt as it should")
	}

	return (*hexutil.Big)(balance.Value), nil
}

// GetCode returns the code stored at the given address in the state for the given block number.
func (b *BlockChainAPI) GetCode(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	return nil, errors.NotSupported
}

// GetProof returns the Merkle-proof for a given account and optionally some storage keys.
func (b *BlockChainAPI) GetProof(
	ctx context.Context,
	address common.Address,
	storageKeys []string,
	blockNumberOrHash rpc.BlockNumberOrHash,
) (*AccountResult, error) {
	return nil, errors.NotSupported
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
	return nil, errors.NotSupported
}

// GetTransactionCount returns the number of transactions the given address has sent for the given block number
func (b *BlockChainAPI) GetTransactionCount(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*hexutil.Uint64, error) {
	nonce := b.Store.GetAccountNonce(ctx, address)
	return (*hexutil.Uint64)(&nonce), nil
}

// GetTransactionByHash returns the transaction for the given hash
func (b *BlockChainAPI) GetTransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*RPCTransaction, error) {
	tx, err := b.transactions.Get(hash)
	if err != nil {
		return nil, err
	}

	rcp, err := b.receipt.GetByTransactionID(tx.Hash())
	if err != nil {
		return nil, err
	}

	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return nil, err
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
) *RPCTransaction {
	block, err := b.blocks.GetByID(blockHash)
	if err != nil {
		return nil
	}

	highestIndex := len(block.TransactionHashes) - 1
	if index > hexutil.Uint(highestIndex) {
		return nil
	}

	txHash := block.TransactionHashes[index]
	tx, err := b.GetTransactionByHash(ctx, txHash)
	if err != nil {
		return nil
	}

	return tx
}

// GetTransactionByBlockNumberAndIndex returns the transaction for the given block number and index.
func (b *BlockChainAPI) GetTransactionByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) *RPCTransaction {
	block, err := b.blocks.GetByHeight(uint64(blockNumber))
	if err != nil {
		return nil
	}

	highestIndex := len(block.TransactionHashes) - 1
	if index > hexutil.Uint(highestIndex) {
		return nil
	}

	txHash := block.TransactionHashes[index]
	tx, err := b.GetTransactionByHash(ctx, txHash)
	if err != nil {
		return nil
	}

	return tx
}

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (b *BlockChainAPI) GetTransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (map[string]interface{}, error) {
	// TODO(m-Peter) These are mock data just for fleshing out the
	// fields involved in the returned value. Ideally we would like to
	// defined interfaces for the storage & indexer services, so that
	// we can proceed with some tests, and get rid of these mock data.
	receipt := map[string]interface{}{}

	txReceipt, err := b.receipt.GetByTransactionID(hash)
	if err != nil {
		return receipt, err
	}

	receipt["blockNumber"] = (*hexutil.Big)(big.NewInt(int64(txReceipt.BlockHeight)))
	receipt["transactionHash"] = common.HexToHash(txReceipt.TxHash)

	if txReceipt.Failed {
		receipt["status"] = hexutil.Uint64(0)
	} else {
		receipt["status"] = hexutil.Uint64(1)
	}

	receipt["type"] = hexutil.Uint64(txReceipt.TxType)
	receipt["gasUsed"] = hexutil.Uint64(txReceipt.GasConsumed)
	receipt["contractAddress"] = common.HexToAddress(txReceipt.DeployedContractAddress)

	logs := []*types.Log{}
	if txReceipt.Logs != "" {
		decodedLogs, err := hex.DecodeString(txReceipt.Logs)
		if err != nil {
			return receipt, err
		}
		err = rlp.Decode(bytes.NewReader(decodedLogs), &logs)
		if err != nil {
			return receipt, err
		}
		receipt["logs"] = logs
	}
	receipt["logsBloom"] = hexutil.Bytes(types.LogsBloom(logs))

	txBytes, err := hex.DecodeString(txReceipt.Transaction)
	if err != nil {
		return receipt, err
	}
	gethTx, err := GethTxFromBytes(txBytes)
	if err != nil {
		return receipt, err
	}
	from, err := types.Sender(types.LatestSignerForChainID(gethTx.ChainId()), gethTx)
	if err != nil {
		return receipt, err
	}
	receipt["from"] = from
	receipt["to"] = gethTx.To()

	block, err := b.Store.GetBlockByNumber(ctx, txReceipt.BlockHeight)
	if err != nil {
		return receipt, err
	}
	receipt["blockHash"] = common.HexToHash(block.Hash)

	txIndex := uint64(0)
	receipt["transactionIndex"] = (*hexutil.Uint64)(&txIndex)
	receipt["cumulativeGasUsed"] = hexutil.Uint64(50000)
	receipt["effectiveGasPrice"] = (*hexutil.Big)(big.NewInt(20000000000))

	return receipt, nil
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
) (map[string]interface{}, error) {
	block := map[string]interface{}{}

	bl, err := b.blocks.GetByID(hash)
	if err != nil {
		return nil, err
	}

	block["number"] = hexutil.Uint64(bl.Height)
	block["hash"] = bl.Hash
	block["parentHash"] = bl.ParentBlockHash
	block["receiptsRoot"] = bl.ReceiptRoot
	block["transactions"] = bl.TransactionHashes

	return block, nil
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
) (map[string]interface{}, error) {
	block := map[string]interface{}{}

	height := uint64(blockNumber)
	var err error
	if blockNumber == -2 {
		height, err = b.blocks.LatestHeight()
		if err != nil {
			return nil, err
		}
	} else if blockNumber < 0 {
		return nil, fmt.Errorf("not supported")
	}

	bl, err := b.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	block["number"] = hexutil.Uint64(bl.Height)
	block["hash"] = bl.Hash
	block["parentHash"] = bl.ParentBlockHash
	block["receiptsRoot"] = bl.ReceiptRoot
	block["transactions"] = bl.TransactionHashes

	return block, nil
}

// GetBlockReceipts returns the block receipts for the given block hash or number or tag.
func (b *BlockChainAPI) GetBlockReceipts(
	ctx context.Context,
	blockNumberOrHash rpc.BlockNumberOrHash,
) ([]map[string]interface{}, error) {
	receipts := make([]map[string]interface{}, 0)

	var block *storage.BlockExecutedPayload
	var err error
	if blockNumberOrHash.BlockHash != nil {
		block, err = b.Store.GetBlockByHash(ctx, *blockNumberOrHash.BlockHash)
		if err != nil {
			return receipts, err
		}
	} else if blockNumberOrHash.BlockNumber != nil {
		block, err = b.Store.GetBlockByNumber(ctx, uint64(blockNumberOrHash.BlockNumber.Int64()))
		if err != nil {
			return receipts, err
		}
	} else {
		return receipts, fmt.Errorf("block number or hash not provided")
	}

	for _, tx := range block.TransactionHashes {
		txHash := common.HexToHash(tx)
		txReceipt, err := b.GetTransactionReceipt(ctx, txHash)
		if err != nil {
			return receipts, err
		}
		receipts = append(receipts, txReceipt)
	}

	return receipts, nil
}

// GetBlockTransactionCountByHash returns the number of transactions in the block with the given hash.
func (b *BlockChainAPI) GetBlockTransactionCountByHash(
	ctx context.Context,
	blockHash common.Hash,
) *hexutil.Uint {
	block, err := b.Store.GetBlockByHash(ctx, blockHash)
	if err != nil {
		return nil
	}

	count := hexutil.Uint(len(block.TransactionHashes))
	return &count
}

// GetBlockTransactionCountByNumber returns the number of transactions in the block with the given block number.
func (b *BlockChainAPI) GetBlockTransactionCountByNumber(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
) *hexutil.Uint {
	// todo handle block number for negative special values in all APIs
	block, err := b.blocks.GetByHeight(uint64(blockNumber))
	if err != nil {
		return nil
	}

	count := hexutil.Uint(len(block.TransactionHashes))
	return &count
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

// GetLogs returns logs matching the given argument that are stored within the state.
func (b *BlockChainAPI) GetLogs(
	ctx context.Context,
	criteria filters.FilterCriteria,
) ([]*types.Log, error) {
	if len(criteria.Topics) > maxTopics {
		return nil, errExceedMaxTopics
	}

	logs := []*types.Log{}
	for _, topicList := range criteria.Topics {
		for _, topic := range topicList {
			matchingLogs := b.Store.LogsByTopic(topic.Hex())
			logs = append(logs, matchingLogs...)
		}
	}

	return logs, nil
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
	return "", errors.NotSupported
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
	return nil, errors.NotSupported
}

// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (b *BlockChainAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	return nil, errors.NotSupported
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
	return nil, errors.NotSupported
}

// SignTransaction will sign the given transaction with the from account.
// The node needs to have the private key of the account corresponding with
// the given from address and it needs to be unlocked.
func (b *BlockChainAPI) SignTransaction(
	ctx context.Context,
	args TransactionArgs,
) (*SignTransactionResult, error) {
	return nil, errors.NotSupported
}

// SendTransaction creates a transaction for the given argument, sign it
// and submit it to the transaction pool.
func (b *BlockChainAPI) SendTransaction(
	ctx context.Context,
	args TransactionArgs,
) (common.Hash, error) {
	return common.Hash{}, errors.NotSupported
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
	txBytes, err := hex.DecodeString(args.Input.String()[2:])
	if err != nil {
		return hexutil.Bytes{}, err
	}
	txData := CadenceByteArrayFromBytes(txBytes)
	toAddress := CadenceEVMAddressFromBytes(args.To.Bytes())

	value, err := b.FlowClient.ExecuteScriptAtLatestBlock(
		ctx,
		BridgedAccountCall,
		[]cadence.Value{txData, toAddress},
	)
	if err != nil {
		return hexutil.Bytes{}, err
	}

	cadenceArray, ok := value.(cadence.Array)
	if !ok {
		return hexutil.Bytes{}, fmt.Errorf("script doesn't return byte array as it should")
	}

	resultValue := make([]byte, len(cadenceArray.Values))
	for i, x := range cadenceArray.Values {
		resultValue[i] = x.ToGoValue().(byte)
	}

	return resultValue, nil
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
	return 0, errors.NotSupported
}

// GetUncleByBlockHashAndIndex returns the uncle block for the given block hash and index.
func (b *BlockChainAPI) GetUncleByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return nil, errors.NotSupported
}

// GetUncleByBlockNumberAndIndex returns the uncle block for the given block hash and index.
func (b *BlockChainAPI) GetUncleByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return nil, errors.NotSupported
}
