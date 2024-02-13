package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"github.com/onflow/flow-evm-gateway/config"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/storage"
)

const EthNamespace = "eth"
const defaultGasPrice = 8049999872

// TODO: Fetch these from flow-go/fvm/evm/emulator/config.go
var (
	FlowEVMTestnetChainID = big.NewInt(666)
	FlowEVMMainnetChainID = big.NewInt(777)
)

func SupportedAPIs(config *config.Config, store *storage.Store) []rpc.API {
	return []rpc.API{
		{
			Namespace: EthNamespace,
			Service:   NewBlockChainAPI(config, store),
		},
	}
}

type BlockChainAPI struct {
	config *config.Config
	Store  *storage.Store
}

func NewBlockChainAPI(config *config.Config, store *storage.Store) *BlockChainAPI {
	return &BlockChainAPI{
		config: config,
		Store:  store,
	}
}

// eth_chainId
// ChainId is the EIP-155 replay-protection chain id for the current Ethereum chain config.
//
// Note, this method does not conform to EIP-695 because the configured chain ID is always
// returned, regardless of the current head block. We used to return an error when the chain
// wasn't synced up to a block where EIP-155 is enabled, but this behavior caused issues
// in CL clients.
func (api *BlockChainAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(api.config.ChainID)
}

// eth_blockNumber
// BlockNumber returns the block number of the chain head.
func (api *BlockChainAPI) BlockNumber() hexutil.Uint64 {
	latestBlockHeight, err := api.Store.LatestBlockHeight(context.Background())
	if err != nil {
		// TODO(m-Peter) We should add a logger to BlockChainAPI
		panic(fmt.Errorf("failed to fetch the latest block number: %v", err))
	}
	return hexutil.Uint64(latestBlockHeight)
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
	input hexutil.Bytes,
) (common.Hash, error) {
	return crypto.Keccak256Hash([]byte("hello world")), nil
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
	return (*hexutil.Big)(big.NewInt(defaultGasPrice)), nil
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
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	code, _ := hex.DecodeString("600160008035811a818181146012578301005b601b6001356025565b8060005260206000f25b600060078202905091905056")
	return hexutil.Bytes(code), nil
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
	storageSlot string,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (hexutil.Bytes, error) {
	storage, _ := hex.DecodeString("600160008035811a818181146012578301005b601b6001356025565b8060005260206000f25b600060078202905091905056")
	return hexutil.Bytes(storage), nil
}

// eth_getTransactionCount (returns the number of tx sent from an address (nonce))
// GetTransactionCount returns the number of transactions the given address has sent for the given block number
func (s *BlockChainAPI) GetTransactionCount(
	ctx context.Context,
	address common.Address,
	blockNumberOrHash *rpc.BlockNumberOrHash,
) (*hexutil.Uint64, error) {
	nonce := s.Store.GetAccountNonce(context.Background(), address)
	return (*hexutil.Uint64)(&nonce), nil
}

// eth_getTransactionByHash
// GetTransactionByHash returns the transaction for the given hash
func (s *BlockChainAPI) GetTransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*RPCTransaction, error) {
	blockHash := common.HexToHash("0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2")
	to := common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb")
	index := uint64(64)

	tx := &RPCTransaction{
		BlockHash:        (*common.Hash)(&blockHash),
		BlockNumber:      (*hexutil.Big)(big.NewInt(6139707)),
		From:             common.HexToAddress("0xa7d9ddbe1f17865597fbd27ec712455208b6b76d"),
		Gas:              hexutil.Uint64(50000),
		GasPrice:         (*hexutil.Big)(big.NewInt(20000000000)),
		Hash:             common.HexToHash("0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b"),
		Input:            hexutil.Bytes("0x68656c6c6f21"),
		Nonce:            hexutil.Uint64(21),
		To:               &to,
		TransactionIndex: (*hexutil.Uint64)(&index),
		Value:            (*hexutil.Big)(big.NewInt(4290000000000000)),
		V:                (*hexutil.Big)(big.NewInt(37)),
		R:                (*hexutil.Big)(big.NewInt(150)),
		S:                (*hexutil.Big)(big.NewInt(250)),
	}
	return tx, nil
}

// eth_getTransactionByBlockHashAndIndex
// GetTransactionByBlockHashAndIndex returns the transaction for the given block hash and index.
func (s *BlockChainAPI) GetTransactionByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) *RPCTransaction {
	to := common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb")
	txIndex := uint64(64)

	tx := &RPCTransaction{
		BlockHash:        (*common.Hash)(&blockHash),
		BlockNumber:      (*hexutil.Big)(big.NewInt(6139707)),
		From:             common.HexToAddress("0xa7d9ddbe1f17865597fbd27ec712455208b6b76d"),
		Gas:              hexutil.Uint64(50000),
		GasPrice:         (*hexutil.Big)(big.NewInt(20000000000)),
		Hash:             common.HexToHash("0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b"),
		Input:            hexutil.Bytes("0x68656c6c6f21"),
		Nonce:            hexutil.Uint64(21),
		To:               &to,
		TransactionIndex: (*hexutil.Uint64)(&txIndex),
		Value:            (*hexutil.Big)(big.NewInt(4290000000000000)),
		V:                (*hexutil.Big)(big.NewInt(37)),
		R:                (*hexutil.Big)(big.NewInt(150)),
		S:                (*hexutil.Big)(big.NewInt(250)),
	}
	return tx
}

// eth_getTransactionByBlockNumberAndIndex
// GetTransactionByBlockNumberAndIndex returns the transaction for the given block number and index.
func (s *BlockChainAPI) GetTransactionByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) *RPCTransaction {
	blockHash := common.HexToHash("0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2")
	to := common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb")
	txIndex := uint64(64)

	tx := &RPCTransaction{
		BlockHash:        (*common.Hash)(&blockHash),
		BlockNumber:      (*hexutil.Big)(big.NewInt(6139707)),
		From:             common.HexToAddress("0xa7d9ddbe1f17865597fbd27ec712455208b6b76d"),
		Gas:              hexutil.Uint64(50000),
		GasPrice:         (*hexutil.Big)(big.NewInt(20000000000)),
		Hash:             common.HexToHash("0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b"),
		Input:            hexutil.Bytes("0x68656c6c6f21"),
		Nonce:            hexutil.Uint64(21),
		To:               &to,
		TransactionIndex: (*hexutil.Uint64)(&txIndex),
		Value:            (*hexutil.Big)(big.NewInt(4290000000000000)),
		V:                (*hexutil.Big)(big.NewInt(37)),
		R:                (*hexutil.Big)(big.NewInt(150)),
		S:                (*hexutil.Big)(big.NewInt(250)),
	}
	return tx
}

// eth_getTransactionReceipt
// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (s *BlockChainAPI) GetTransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (map[string]interface{}, error) {
	// TODO(m-Peter) These are mock data just for fleshing out the
	// fields involved in the returned value. Ideally we would like to
	// defined interfaces for the storage & indexer services, so that
	// we can proceed with some tests, and get rid of these mock data.
	receipt := map[string]interface{}{}

	txReceipt, err := s.Store.GetTransactionByHash(ctx, hash)
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
	decodedLogs, err := hex.DecodeString(txReceipt.Logs)
	if err != nil {
		return receipt, err
	}
	err = rlp.Decode(bytes.NewReader(decodedLogs), &logs)
	if err != nil {
		return receipt, err
	}
	receipt["logs"] = logs
	receipt["logsBloom"] = hexutil.Bytes(types.LogsBloom(logs))

	decodedTx, err := hex.DecodeString(txReceipt.Transaction)
	if err != nil {
		return receipt, err
	}
	tx := &types.Transaction{}
	encodedLen := uint(len(txReceipt.Transaction))
	err = tx.DecodeRLP(
		rlp.NewStream(
			bytes.NewReader(decodedTx),
			uint64(encodedLen),
		),
	)
	if err != nil {
		return receipt, err
	}
	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return receipt, err
	}
	receipt["from"] = from
	receipt["to"] = tx.To()

	txIndex := uint64(0)
	receipt["transactionIndex"] = (*hexutil.Uint64)(&txIndex)
	receipt["blockHash"] = common.HexToHash("0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2")
	receipt["cumulativeGasUsed"] = hexutil.Uint64(50000)
	receipt["effectiveGasPrice"] = (*hexutil.Big)(big.NewInt(20000000000))

	return receipt, nil
}

// eth_coinbase (return the coinbase for a block)
// Coinbase is the address that mining rewards will be sent to (alias for Etherbase).
func (s *BlockChainAPI) Coinbase() (common.Address, error) {
	return s.config.Coinbase, nil
}

// eth_getBlockByHash
// GetBlockByHash returns the requested block. When fullTx is true all transactions in the block are returned in full
// detail, otherwise only the transaction hash is returned.
func (s *BlockChainAPI) GetBlockByHash(
	ctx context.Context,
	hash common.Hash,
	fullTx bool,
) (map[string]interface{}, error) {
	block := map[string]interface{}{}
	block["difficulty"] = "0x4ea3f27bc"
	block["extraData"] = "0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32"
	block["gasLimit"] = "0x1388"
	block["gasUsed"] = "0x0"
	block["hash"] = "0xdc0818cf78f21a8e70579cb46a43643f78291264dda342ae31049421c82d21ae"
	block["logsBloom"] = "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	block["miner"] = "0xbb7b8287f3f0a933474a79eae42cbca977791171"
	block["mixHash"] = "0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843"
	block["nonce"] = "0x689056015818adbe"
	block["number"] = "0x1b4"
	block["parentHash"] = "0xe99e022112df268087ea7eafaf4790497fd21dbeeb6bd7a1721df161a6657a54"
	block["receiptsRoot"] = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	block["sha3Uncles"] = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
	block["size"] = "0x220"
	block["stateRoot"] = "0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d"
	block["timestamp"] = "0x55ba467c"
	block["totalDifficulty"] = "0x78ed983323d"
	block["transactions"] = []string{}
	block["transactionsRoot"] = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	block["uncles"] = []string{}
	return block, nil
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
	block := map[string]interface{}{}

	blockExecutedPayload, err := s.Store.GetBlockByNumber(ctx, uint64(blockNumber))
	if err != nil {
		return block, err
	}

	block["number"] = hexutil.Uint64(blockExecutedPayload.Height)
	block["hash"] = blockExecutedPayload.Hash
	block["parentHash"] = blockExecutedPayload.ParentBlockHash
	block["receiptsRoot"] = blockExecutedPayload.ReceiptRoot
	block["transactions"] = blockExecutedPayload.TransactionHashes

	block["difficulty"] = "0x4ea3f27bc"
	block["extraData"] = "0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32"
	block["gasLimit"] = "0x1388"
	block["gasUsed"] = "0x0"
	block["logsBloom"] = "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	block["miner"] = "0xbb7b8287f3f0a933474a79eae42cbca977791171"
	block["mixHash"] = "0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843"
	block["nonce"] = "0x689056015818adbe"
	block["sha3Uncles"] = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
	block["size"] = "0x220"
	block["stateRoot"] = "0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d"
	block["timestamp"] = "0x55ba467c"
	block["totalDifficulty"] = "0x78ed983323d"
	block["transactionsRoot"] = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	block["uncles"] = []string{}

	return block, nil
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

	logs := []*types.Log{}
	for _, topicList := range criteria.Topics {
		for _, topic := range topicList {
			matchingLogs := s.Store.LogsByTopic(topic.Hex())
			logs = append(logs, matchingLogs...)
		}
	}

	return logs, nil
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
	return rpc.ID("filter0"), nil
}

// eth_uninstallFilter
// UninstallFilter removes the filter with the given filter id.
func (s *BlockChainAPI) UninstallFilter(id rpc.ID) bool {
	return true
}

// eth_getFilterLogs
// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found an empty array of logs is returned.
func (s *BlockChainAPI) GetFilterLogs(
	ctx context.Context,
	id rpc.ID,
) ([]*types.Log, error) {
	log := &types.Log{
		Index:       1,
		BlockNumber: 436,
		BlockHash:   common.HexToHash("0x8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcfdf829c5a142f1fccd7d"),
		TxHash:      common.HexToHash("0xdf829c5a142f1fccd7d8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcf"),
		TxIndex:     0,
		Address:     common.HexToAddress("0x16c5785ac562ff41e2dcfdf829c5a142f1fccd7d"),
		Data:        []byte{0, 0, 0},
		Topics:      []common.Hash{common.HexToHash("0x59ebeb90bc63057b6515673c3ecf9438e5058bca0f92585014eced636878c9a5")},
	}

	return []*types.Log{log}, nil
}

// eth_getFilterChanges
// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (s *BlockChainAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	if id == rpc.ID("") {
		return nil, errFilterNotFound
	}
	log := &types.Log{
		Index:       1,
		BlockNumber: 436,
		BlockHash:   common.HexToHash("0x8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcfdf829c5a142f1fccd7d"),
		TxHash:      common.HexToHash("0xdf829c5a142f1fccd7d8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcf"),
		TxIndex:     0,
		Address:     common.HexToAddress("0x16c5785ac562ff41e2dcfdf829c5a142f1fccd7d"),
		Data:        []byte{0, 0, 0},
		Topics:      []common.Hash{common.HexToHash("0x59ebeb90bc63057b6515673c3ecf9438e5058bca0f92585014eced636878c9a5")},
	}

	return []*types.Log{log}, nil
}

// eth_newBlockFilter
// NewBlockFilter creates a filter that fetches blocks that are imported into the chain.
// It is part of the filter package since polling goes with eth_getFilterChanges.
func (s *BlockChainAPI) NewBlockFilter() rpc.ID {
	return rpc.ID("block_filter")
}

// eth_newPendingTransactionFilter
// NewPendingTransactionFilter creates a filter that fetches pending transactions
// as transactions enter the pending state.
//
// It is part of the filter package because this filter can be used through the
// `eth_getFilterChanges` polling method that is also used for log filters.
func (s *BlockChainAPI) NewPendingTransactionFilter(fullTx *bool) rpc.ID {
	return rpc.ID("pending_tx_filter")
}

// eth_accounts
// Accounts returns the collection of accounts this node manages.
func (s *BlockChainAPI) Accounts() []common.Address {
	return []common.Address{}
}

// eth_sign
// Sign calculates an ECDSA signature for:
// keccak256("\x19Ethereum Signed Message:\n" + len(message) + message).
//
// Note, the produced signature conforms to the secp256k1 curve R, S and V values,
// where the V value will be 27 or 28 for legacy reasons.
//
// The account associated with addr must be unlocked.
//
// https://github.com/ethereum/wiki/wiki/JSON-RPC#eth_sign
func (s *BlockChainAPI) Sign(
	addr common.Address,
	data hexutil.Bytes,
) (hexutil.Bytes, error) {
	return hexutil.Bytes{}, fmt.Errorf("method is not implemented")
}

// eth_signTransaction
// SignTransaction will sign the given transaction with the from account.
// The node needs to have the private key of the account corresponding with
// the given from address and it needs to be unlocked.
func (s *BlockChainAPI) SignTransaction(
	ctx context.Context,
	args TransactionArgs,
) (*SignTransactionResult, error) {
	return &SignTransactionResult{}, fmt.Errorf("method is not implemented")
}

// eth_sendTransaction
// SendTransaction creates a transaction for the given argument, sign it
// and submit it to the transaction pool.
func (s *BlockChainAPI) SendTransaction(
	ctx context.Context,
	args TransactionArgs,
) (common.Hash, error) {
	return common.Hash{}, fmt.Errorf("method is not implemented")
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
	return hexutil.Bytes{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, nil
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

// eth_getUncleByBlockHashAndIndex
// GetUncleByBlockHashAndIndex returns the uncle block for the given block hash and index.
func (s *BlockChainAPI) GetUncleByBlockHashAndIndex(
	ctx context.Context,
	blockHash common.Hash,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// eth_getUncleByBlockNumberAndIndex
// GetUncleByBlockNumberAndIndex returns the uncle block for the given block hash and index.
func (s *BlockChainAPI) GetUncleByBlockNumberAndIndex(
	ctx context.Context,
	blockNumber rpc.BlockNumber,
	index hexutil.Uint,
) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
