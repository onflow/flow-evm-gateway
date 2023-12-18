package api_test

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Account struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func TestBlockChainAPI(t *testing.T) {

	t.Parallel()

	store := storage.NewStore()
	blockchainAPI := &api.BlockChainAPI{
		Store: store,
	}

	t.Run("ChainId", func(t *testing.T) {
		chainID := blockchainAPI.ChainId()

		assert.Equal(t, (*hexutil.Big)(emulator.FlowEVMTestnetChainID), chainID)
	})

	t.Run("BlockNumber", func(t *testing.T) {
		blockNumber := blockchainAPI.BlockNumber()

		assert.Equal(t, hexutil.Uint64(0), blockNumber)
	})

	t.Run("Syncing", func(t *testing.T) {
		syncing, err := blockchainAPI.Syncing()
		require.NoError(t, err)

		isSyncing := syncing.(bool)
		assert.False(t, isSyncing)
	})

	t.Run("SendRawTransaction", func(t *testing.T) {
		hash, err := blockchainAPI.SendRawTransaction(
			context.Background(),
			hexutil.Bytes{},
		)
		require.NoError(t, err)

		assert.Equal(t, common.Hash{}, hash)
	})

	t.Run("CreateAccessList", func(t *testing.T) {
		key1, _ := crypto.GenerateKey()
		addr1 := crypto.PubkeyToAddress(key1.PublicKey)
		from := Account{key: key1, addr: addr1}
		key2, _ := crypto.GenerateKey()
		addr2 := crypto.PubkeyToAddress(key1.PublicKey)
		to := Account{key: key2, addr: addr2}
		accessListResult, err := blockchainAPI.CreateAccessList(
			context.Background(),
			api.TransactionArgs{
				From:  &from.addr,
				To:    &to.addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			nil,
		)
		require.NoError(t, err)

		assert.Equal(t, accessListResult.GasUsed, hexutil.Uint64(105))
	})

	t.Run("FeeHistory", func(t *testing.T) {
		feeHistoryResult, err := blockchainAPI.FeeHistory(
			context.Background(),
			math.HexOrDecimal64(150),
			rpc.BlockNumber(120),
			[]float64{0.02, 0.05},
		)
		require.NoError(t, err)

		assert.Equal(t, feeHistoryResult.OldestBlock, (*hexutil.Big)(big.NewInt(10102020506)))
		assert.Equal(t, feeHistoryResult.GasUsedRatio, []float64{105.0})
	})

	t.Run("GasPrice", func(t *testing.T) {
		gasPrice, err := blockchainAPI.GasPrice(context.Background())
		require.NoError(t, err)

		assert.Equal(t, gasPrice, (*hexutil.Big)(big.NewInt(10102020506)))
	})

	t.Run("MaxPriorityFeePerGas", func(t *testing.T) {
		maxFeePerGas, err := blockchainAPI.MaxPriorityFeePerGas(context.Background())
		require.NoError(t, err)

		assert.Equal(t, maxFeePerGas, (*hexutil.Big)(big.NewInt(10102020506)))
	})

	t.Run("GetBalance", func(t *testing.T) {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		balance, err := blockchainAPI.GetBalance(
			context.Background(),
			addr,
			nil,
		)
		require.NoError(t, err)

		assert.Equal(t, balance, (*hexutil.Big)(big.NewInt(101)))
	})

	t.Run("GetProof", func(t *testing.T) {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		accountResult, err := blockchainAPI.GetProof(
			context.Background(),
			addr,
			[]string{"key1", "key2"},
			rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber),
		)
		require.NoError(t, err)

		assert.Equal(t, accountResult.Balance, (*hexutil.Big)(big.NewInt(10011)))
	})

	t.Run("GetStorageAt", func(t *testing.T) {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		blockNumberOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		storage, err := blockchainAPI.GetStorageAt(
			context.Background(),
			addr,
			"slot1",
			&blockNumberOrHash,
		)
		require.NoError(t, err)

		assert.Equal(t, hexutil.Bytes{}, storage)
	})

	t.Run("GetTransactionCount", func(t *testing.T) {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		blockNumberOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		txCount, err := blockchainAPI.GetTransactionCount(
			context.Background(),
			addr,
			&blockNumberOrHash,
		)
		require.NoError(t, err)

		nonce := uint64(1050510)
		assert.Equal(t, txCount, (*hexutil.Uint64)(&nonce))
	})

	t.Run("GetTransactionByHash", func(t *testing.T) {
		tx, err := blockchainAPI.GetTransactionByHash(
			context.Background(),
			common.Hash{0, 1, 2},
		)
		require.NoError(t, err)

		assert.Equal(t, &api.RPCTransaction{}, tx)
	})

	t.Run("GetTransactionByBlockHashAndIndex", func(t *testing.T) {
		tx := blockchainAPI.GetTransactionByBlockHashAndIndex(
			context.Background(),
			common.Hash{0, 1, 2},
			hexutil.Uint(105),
		)

		assert.Equal(t, &api.RPCTransaction{}, tx)
	})

	t.Run("GetTransactionByBlockNumberAndIndex", func(t *testing.T) {
		tx := blockchainAPI.GetTransactionByBlockNumberAndIndex(
			context.Background(),
			rpc.LatestBlockNumber,
			hexutil.Uint(105),
		)

		assert.Equal(t, &api.RPCTransaction{}, tx)
	})

	t.Run("GetTransactionReceipt", func(t *testing.T) {
		receipt, err := blockchainAPI.GetTransactionReceipt(
			context.Background(),
			common.Hash{0, 1, 2},
		)
		require.NoError(t, err)

		assert.Equal(t, map[string]interface{}{}, receipt)
	})

	t.Run("Coinbase", func(t *testing.T) {
		addr, err := blockchainAPI.Coinbase()
		require.NoError(t, err)

		assert.Equal(t, common.Address{1, 2, 3, 4, 5}, addr)
	})

	t.Run("GetBlockByHash", func(t *testing.T) {
		block, err := blockchainAPI.GetBlockByHash(
			context.Background(),
			common.Hash{0, 1, 2},
			false,
		)
		require.NoError(t, err)

		assert.Equal(t, map[string]interface{}{}, block)
	})

	t.Run("GetBlockByNumber", func(t *testing.T) {
		block, err := blockchainAPI.GetBlockByNumber(
			context.Background(),
			rpc.PendingBlockNumber,
			false,
		)
		require.NoError(t, err)

		assert.Equal(t, map[string]interface{}{}, block)
	})

	t.Run("GetBlockReceipts", func(t *testing.T) {
		receipts, err := blockchainAPI.GetBlockReceipts(
			context.Background(),
			rpc.BlockNumberOrHashWithNumber(rpc.FinalizedBlockNumber),
		)
		require.NoError(t, err)

		assert.Equal(t, make([]map[string]interface{}, 0), receipts)
	})

	t.Run("GetBlockTransactionCountByHash", func(t *testing.T) {
		blockTxCount := blockchainAPI.GetBlockTransactionCountByHash(
			context.Background(),
			common.Hash{0, 1, 2},
		)

		count := hexutil.Uint(100522)
		assert.Equal(t, &count, blockTxCount)
	})

	t.Run("GetBlockTransactionCountByNumber", func(t *testing.T) {
		blockTxCount := blockchainAPI.GetBlockTransactionCountByNumber(
			context.Background(),
			rpc.FinalizedBlockNumber,
		)

		count := hexutil.Uint(522)
		assert.Equal(t, &count, blockTxCount)
	})

	t.Run("GetUncleCountByBlockHash", func(t *testing.T) {
		uncleCount := blockchainAPI.GetUncleCountByBlockHash(
			context.Background(),
			common.Hash{0, 1, 2},
		)

		count := hexutil.Uint(0)
		assert.Equal(t, &count, uncleCount)
	})

	t.Run("GetUncleCountByBlockNumber", func(t *testing.T) {
		uncleCount := blockchainAPI.GetUncleCountByBlockNumber(
			context.Background(),
			rpc.FinalizedBlockNumber,
		)

		count := hexutil.Uint(0)
		assert.Equal(t, &count, uncleCount)
	})

	t.Run("GetLogs", func(t *testing.T) {
		logs, err := blockchainAPI.GetLogs(
			context.Background(),
			filters.FilterCriteria{},
		)
		require.NoError(t, err)

		assert.Equal(t, []*types.Log{}, logs)
	})

	t.Run("NewFilter", func(t *testing.T) {
		filterID, err := blockchainAPI.NewFilter(
			filters.FilterCriteria{},
		)
		require.NoError(t, err)

		assert.Equal(t, rpc.ID("filter0"), filterID)
	})

	t.Run("UninstallFilter", func(t *testing.T) {
		removed := blockchainAPI.UninstallFilter(rpc.ID("filter0"))

		assert.True(t, removed)
	})

	t.Run("GetFilterLogs", func(t *testing.T) {
		logs, err := blockchainAPI.GetFilterLogs(
			context.Background(),
			rpc.ID("filter0"),
		)
		require.NoError(t, err)

		assert.Equal(t, []*types.Log{}, logs)
	})

	t.Run("GetFilterChanges", func(t *testing.T) {
		changes, err := blockchainAPI.GetFilterChanges(
			rpc.ID("filter"),
		)
		require.NoError(t, err)

		assert.Equal(t, []interface{}{}, changes)
	})

	t.Run("NewBlockFilter", func(t *testing.T) {
		filterID := blockchainAPI.NewBlockFilter()

		assert.Equal(t, rpc.ID("block_filter"), filterID)
	})

	t.Run("NewPendingTransactionFilter", func(t *testing.T) {
		filterID := blockchainAPI.NewPendingTransactionFilter(nil)

		assert.Equal(t, rpc.ID("pending_tx_filter"), filterID)
	})

	t.Run("Accounts", func(t *testing.T) {
		accounts := blockchainAPI.Accounts()

		assert.Equal(t, []common.Address{}, accounts)
	})

	t.Run("Sign", func(t *testing.T) {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		_, err := blockchainAPI.Sign(
			context.Background(),
			hexutil.Bytes{1, 2, 3, 4, 5},
			addr,
			"secret_password",
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not implemented")
	})

	t.Run("SignTransaction", func(t *testing.T) {
		key1, _ := crypto.GenerateKey()
		addr1 := crypto.PubkeyToAddress(key1.PublicKey)
		from := Account{key: key1, addr: addr1}
		key2, _ := crypto.GenerateKey()
		addr2 := crypto.PubkeyToAddress(key1.PublicKey)
		to := Account{key: key2, addr: addr2}
		_, err := blockchainAPI.SignTransaction(
			context.Background(),
			api.TransactionArgs{
				From:  &from.addr,
				To:    &to.addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not implemented")
	})

	t.Run("SendTransaction", func(t *testing.T) {
		key1, _ := crypto.GenerateKey()
		addr1 := crypto.PubkeyToAddress(key1.PublicKey)
		from := Account{key: key1, addr: addr1}
		key2, _ := crypto.GenerateKey()
		addr2 := crypto.PubkeyToAddress(key1.PublicKey)
		to := Account{key: key2, addr: addr2}
		_, err := blockchainAPI.SendTransaction(
			context.Background(),
			api.TransactionArgs{
				From:  &from.addr,
				To:    &to.addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			"secret_password",
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not implemented")
	})

	t.Run("Call", func(t *testing.T) {
		key1, _ := crypto.GenerateKey()
		addr1 := crypto.PubkeyToAddress(key1.PublicKey)
		from := Account{key: key1, addr: addr1}
		key2, _ := crypto.GenerateKey()
		addr2 := crypto.PubkeyToAddress(key1.PublicKey)
		to := Account{key: key2, addr: addr2}
		result, err := blockchainAPI.Call(
			context.Background(),
			api.TransactionArgs{
				From:  &from.addr,
				To:    &to.addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			nil,
			nil,
			nil,
		)
		require.NoError(t, err)

		assert.Equal(t, hexutil.Bytes{}, result)
	})

	t.Run("EstimateGas", func(t *testing.T) {
		key1, _ := crypto.GenerateKey()
		addr1 := crypto.PubkeyToAddress(key1.PublicKey)
		from := Account{key: key1, addr: addr1}
		key2, _ := crypto.GenerateKey()
		addr2 := crypto.PubkeyToAddress(key1.PublicKey)
		to := Account{key: key2, addr: addr2}
		gasEstimate, err := blockchainAPI.EstimateGas(
			context.Background(),
			api.TransactionArgs{
				From:  &from.addr,
				To:    &to.addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			nil,
			nil,
		)
		require.NoError(t, err)

		assert.Equal(t, hexutil.Uint64(105), gasEstimate)
	})
}
