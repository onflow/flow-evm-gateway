package api_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/cadence"
	"github.com/onflow/cadence/runtime/stdlib"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/api/mocks"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type Account struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func TestBlockChainAPI(t *testing.T) {

	t.Parallel()

	store := storage.NewStore()
	config := &api.Config{
		ChainID:  api.FlowEVMTestnetChainID,
		Coinbase: common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"),
	}
	flowClient, err := api.NewFlowClient(grpc.EmulatorHost)
	require.NoError(t, err)
	blockchainAPI := api.NewBlockChainAPI(config, store, flowClient)

	t.Run("ChainId", func(t *testing.T) {
		chainID := blockchainAPI.ChainId()

		assert.Equal(t, (*hexutil.Big)(api.FlowEVMTestnetChainID), chainID)
	})

	t.Run("BlockNumber", func(t *testing.T) {
		blockNumber := blockchainAPI.BlockNumber()
		assert.Equal(t, hexutil.Uint64(0), blockNumber)

		event := blockExecutedEvent(
			1,
			"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9",
			7766279631452241920,
			"0xe81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421c0",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			[]string{"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9"},
		)
		store := blockchainAPI.Store
		err := store.StoreBlock(context.Background(), event)
		require.NoError(t, err)

		blockNumber = blockchainAPI.BlockNumber()
		assert.Equal(t, hexutil.Uint64(1), blockNumber)

		event = blockExecutedEvent(
			2,
			"0xaae4530246e61ae58479824ab0863f99ca50414d27aec0c269ae6a7cfc4c7f5b",
			7766279631452241920,
			"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			[]string{"0xaae4530246e61ae58479824ab0863f99ca50414d27aec0c269ae6a7cfc4c7f5b"},
		)

		err = store.StoreBlock(context.Background(), event)
		require.NoError(t, err)

		blockNumber = blockchainAPI.BlockNumber()
		assert.Equal(t, hexutil.Uint64(2), blockNumber)
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

		assert.Equal(
			t,
			common.HexToHash("0x47173285a8d7341e5e972fc677286384f802f8ef42a5ec5f03bbfa254cb01fad"),
			hash,
		)
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

		assert.Equal(t, gasPrice, (*hexutil.Big)(big.NewInt(8049999872)))
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

		expected, _ := hex.DecodeString("600160008035811a818181146012578301005b601b6001356025565b8060005260206000f25b600060078202905091905056")
		assert.Equal(t, hexutil.Bytes(expected), storage)
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

		nonce := uint64(0)
		assert.Equal(t, txCount, (*hexutil.Uint64)(&nonce))

		event := transactionExecutedEvent(
			3,
			"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c",
			"b88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb",
			false,
			2,
			22514,
			"0000000000000000000000000000000000000000",
			"000000000000000000000000000000000000000000000000000000000000002a",
			"f85af8589499466ed2e37b892a2ee3e9cd55a98b68f5735db2e1a024abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503daa0000000000000000000000000000000000000000000000000000000000000002a",
		)

		store := blockchainAPI.Store
		store.UpdateAccountNonce(context.Background(), event)

		txCount, err = blockchainAPI.GetTransactionCount(
			context.Background(),
			common.HexToAddress("0x658Bdf435d810C91414eC09147DAA6DB62406379"),
			&blockNumberOrHash,
		)
		require.NoError(t, err)

		nonce = uint64(1)
		assert.Equal(t, txCount, (*hexutil.Uint64)(&nonce))
	})

	t.Run("GetTransactionByHash", func(t *testing.T) {
		tx, err := blockchainAPI.GetTransactionByHash(
			context.Background(),
			common.Hash{0, 1, 2},
		)
		require.NoError(t, err)

		blockHash := common.HexToHash("0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2")
		to := common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb")
		index := uint64(64)

		expectedTx := &api.RPCTransaction{
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

		assert.Equal(t, expectedTx, tx)
	})

	t.Run("GetTransactionByBlockHashAndIndex", func(t *testing.T) {
		blockHash := common.Hash{0, 1, 2}
		tx := blockchainAPI.GetTransactionByBlockHashAndIndex(
			context.Background(),
			blockHash,
			hexutil.Uint(105),
		)

		to := common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb")
		txIndex := uint64(64)

		expectedTx := &api.RPCTransaction{
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

		assert.Equal(t, expectedTx, tx)
	})

	t.Run("GetTransactionByBlockNumberAndIndex", func(t *testing.T) {
		tx := blockchainAPI.GetTransactionByBlockNumberAndIndex(
			context.Background(),
			rpc.LatestBlockNumber,
			hexutil.Uint(105),
		)

		blockHash := common.HexToHash("0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2")
		to := common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb")
		txIndex := uint64(64)

		expectedTx := &api.RPCTransaction{
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

		assert.Equal(t, expectedTx, tx)
	})

	t.Run("GetTransactionReceipt", func(t *testing.T) {
		event := transactionExecutedEvent(
			3,
			"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c",
			"b88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb",
			false,
			2,
			22514,
			"0000000000000000000000000000000000000000",
			"000000000000000000000000000000000000000000000000000000000000002a",
			"f85af8589499466ed2e37b892a2ee3e9cd55a98b68f5735db2e1a024abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503daa0000000000000000000000000000000000000000000000000000000000000002a",
		)

		store := blockchainAPI.Store
		store.StoreTransaction(context.Background(), event)

		receipt, err := blockchainAPI.GetTransactionReceipt(
			context.Background(),
			common.HexToHash("0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c"),
		)
		require.NoError(t, err)

		expectedReceipt := map[string]interface{}{}
		expectedReceipt["blockNumber"] = (*hexutil.Big)(big.NewInt(3))
		expectedReceipt["transactionHash"] = common.HexToHash("0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c")
		expectedReceipt["status"] = hexutil.Uint64(1)
		expectedReceipt["type"] = hexutil.Uint64(2)
		expectedReceipt["gasUsed"] = hexutil.Uint64(22514)
		expectedReceipt["contractAddress"] = common.HexToAddress("0x0000000000000000000000000000000000000000")
		expectedReceipt["from"] = common.HexToAddress("0x658Bdf435d810C91414eC09147DAA6DB62406379")
		to := common.HexToAddress("0x99466ED2E37B892A2Ee3E9CD55a98b68f5735db2")
		expectedReceipt["to"] = &to

		txIndex := uint64(0)
		expectedReceipt["transactionIndex"] = (*hexutil.Uint64)(&txIndex)
		expectedReceipt["blockHash"] = common.HexToHash("0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2")
		expectedReceipt["cumulativeGasUsed"] = hexutil.Uint64(50000)
		expectedReceipt["effectiveGasPrice"] = (*hexutil.Big)(big.NewInt(20000000000))

		data, err := hex.DecodeString("000000000000000000000000000000000000000000000000000000000000002a")
		require.NoError(t, err)
		log := &types.Log{
			Index:       0,
			BlockNumber: 0,
			BlockHash:   common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
			TxHash:      common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
			TxIndex:     0,
			Address:     common.HexToAddress("0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2"),
			Data:        data,
			Topics:      []common.Hash{common.HexToHash("0x24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da")},
		}
		logs := []*types.Log{log}
		expectedReceipt["logs"] = logs
		expectedReceipt["logsBloom"] = hexutil.Bytes(types.LogsBloom(logs))

		assert.Equal(t, expectedReceipt, receipt)
	})

	t.Run("Coinbase", func(t *testing.T) {
		addr, err := blockchainAPI.Coinbase()
		require.NoError(t, err)

		assert.Equal(
			t,
			common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"),
			addr,
		)
	})

	t.Run("GetBlockByHash", func(t *testing.T) {
		block, err := blockchainAPI.GetBlockByHash(
			context.Background(),
			common.Hash{0, 1, 2},
			false,
		)
		require.NoError(t, err)

		expectedBlock := map[string]interface{}{}
		expectedBlock["difficulty"] = "0x4ea3f27bc"
		expectedBlock["extraData"] = "0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32"
		expectedBlock["gasLimit"] = "0x1388"
		expectedBlock["gasUsed"] = "0x0"
		expectedBlock["hash"] = "0xdc0818cf78f21a8e70579cb46a43643f78291264dda342ae31049421c82d21ae"
		expectedBlock["logsBloom"] = "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
		expectedBlock["miner"] = "0xbb7b8287f3f0a933474a79eae42cbca977791171"
		expectedBlock["mixHash"] = "0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843"
		expectedBlock["nonce"] = "0x689056015818adbe"
		expectedBlock["number"] = "0x1b4"
		expectedBlock["parentHash"] = "0xe99e022112df268087ea7eafaf4790497fd21dbeeb6bd7a1721df161a6657a54"
		expectedBlock["receiptsRoot"] = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
		expectedBlock["sha3Uncles"] = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
		expectedBlock["size"] = "0x220"
		expectedBlock["stateRoot"] = "0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d"
		expectedBlock["timestamp"] = "0x55ba467c"
		expectedBlock["totalDifficulty"] = "0x78ed983323d"
		expectedBlock["transactions"] = []string{}
		expectedBlock["transactionsRoot"] = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
		expectedBlock["uncles"] = []string{}

		assert.Equal(t, expectedBlock, block)
	})

	t.Run("GetBlockByNumber", func(t *testing.T) {
		event := blockExecutedEvent(
			1,
			"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9",
			7766279631452241920,
			"0xe81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421c0",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			[]string{"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9"},
		)
		store := blockchainAPI.Store
		err := store.StoreBlock(context.Background(), event)
		require.NoError(t, err)

		block, err := blockchainAPI.GetBlockByNumber(
			context.Background(),
			rpc.BlockNumber(1),
			false,
		)
		require.NoError(t, err)

		expectedBlock := map[string]interface{}{}
		expectedBlock["number"] = hexutil.Uint64(1)
		expectedBlock["hash"] = "0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9"
		expectedBlock["parentHash"] = "0xe81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421c0"
		expectedBlock["receiptsRoot"] = "0x0000000000000000000000000000000000000000000000000000000000000000"
		expectedBlock["transactions"] = []string{"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9"}

		expectedBlock["difficulty"] = "0x4ea3f27bc"
		expectedBlock["extraData"] = "0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32"
		expectedBlock["gasLimit"] = "0x1388"
		expectedBlock["gasUsed"] = "0x0"
		expectedBlock["logsBloom"] = "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
		expectedBlock["miner"] = "0xbb7b8287f3f0a933474a79eae42cbca977791171"
		expectedBlock["mixHash"] = "0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843"
		expectedBlock["nonce"] = "0x689056015818adbe"
		expectedBlock["sha3Uncles"] = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
		expectedBlock["size"] = "0x220"
		expectedBlock["stateRoot"] = "0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d"
		expectedBlock["timestamp"] = "0x55ba467c"
		expectedBlock["totalDifficulty"] = "0x78ed983323d"
		expectedBlock["transactionsRoot"] = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
		expectedBlock["uncles"] = []string{}

		assert.Equal(t, expectedBlock, block)
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

		event := transactionExecutedEvent(
			3,
			"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c",
			"b88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb",
			false,
			2,
			22514,
			"0000000000000000000000000000000000000000",
			"000000000000000000000000000000000000000000000000000000000000002a",
			"f85af8589499466ed2e37b892a2ee3e9cd55a98b68f5735db2e1a024abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503daa0000000000000000000000000000000000000000000000000000000000000002a",
		)

		store := blockchainAPI.Store
		store.StoreLog(context.Background(), event)

		logs, err = blockchainAPI.GetLogs(
			context.Background(),
			filters.FilterCriteria{
				Topics: [][]common.Hash{
					{
						common.HexToHash("0x24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da"),
					},
				},
			},
		)
		require.NoError(t, err)

		data, err := hex.DecodeString("000000000000000000000000000000000000000000000000000000000000002a")
		require.NoError(t, err)
		log := &types.Log{
			Index:       0,
			BlockNumber: 0,
			BlockHash:   common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
			TxHash:      common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
			TxIndex:     0,
			Address:     common.HexToAddress("0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2"),
			Data:        data,
			Topics:      []common.Hash{common.HexToHash("0x24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da")},
		}

		assert.Equal(t, []*types.Log{log}, logs)
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

		assert.Equal(t, []*types.Log{log}, logs)
	})

	t.Run("GetFilterChanges", func(t *testing.T) {
		changes, err := blockchainAPI.GetFilterChanges(
			rpc.ID("filter"),
		)
		require.NoError(t, err)

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

		assert.Equal(t, []*types.Log{log}, changes)
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

		assert.Equal(
			t,
			[]common.Address{},
			accounts,
		)
	})

	t.Run("Sign", func(t *testing.T) {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		_, err := blockchainAPI.Sign(
			addr,
			hexutil.Bytes{1, 2, 3, 4, 5},
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
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not implemented")
	})

	t.Run("Call", func(t *testing.T) {
		mockFlowClient := new(mocks.MockAccessClient)
		blockchainAPI = api.NewBlockChainAPI(config, store, mockFlowClient)

		from := common.HexToAddress("0x658bdf435d810c91414ec09147daa6db62406379")
		to := common.HexToAddress("0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2")
		gas := hexutil.Uint64(21500)
		gasPrice := hexutil.Big(*big.NewInt(1350000))
		value := hexutil.Big(*big.NewInt(0))
		input := hexutil.Bytes("0xc6888fa10000000000000000000000000000000000000000000000000000000000000006")

		result, err := hex.DecodeString("000000000000000000000000000000000000000000000000000000000000002a")
		require.NoError(t, err)
		toBytes := make([]cadence.Value, 0)
		for _, bt := range result {
			toBytes = append(toBytes, cadence.UInt8(bt))
		}
		returnValue := cadence.NewArray(
			toBytes,
		).WithType(cadence.NewVariableSizedArrayType(cadence.TheUInt8Type))
		mockFlowClient.On("ExecuteScriptAtLatestBlock", mock.Anything, mock.Anything, mock.Anything).Return(returnValue, nil)

		returnedValue, err := blockchainAPI.Call(
			context.Background(),
			api.TransactionArgs{
				From:     &from,
				To:       &to,
				Gas:      &gas,
				GasPrice: &gasPrice,
				Value:    &value,
				Input:    &input,
			},
			nil,
			nil,
			nil,
		)
		require.NoError(t, err)

		assert.Equal(
			t,
			hexutil.Bytes{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2a},
			returnedValue,
		)
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

	t.Run("GetUncleByBlockHashAndIndex", func(t *testing.T) {
		uncle, err := blockchainAPI.GetUncleByBlockHashAndIndex(
			context.Background(),
			common.HexToHash("0x8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcfdf829c5a142f1fccd7d"),
			hexutil.Uint(105),
		)
		require.NoError(t, err)

		assert.Equal(t, map[string]interface{}{}, uncle)
	})

	t.Run("GetUncleByBlockNumberAndIndex", func(t *testing.T) {
		uncle, err := blockchainAPI.GetUncleByBlockNumberAndIndex(
			context.Background(),
			rpc.FinalizedBlockNumber,
			hexutil.Uint(115),
		)
		require.NoError(t, err)

		assert.Equal(t, map[string]interface{}{}, uncle)
	})

	t.Run("Sign", func(t *testing.T) {
		_, err := blockchainAPI.Sign(
			common.HexToAddress("0x16c5785ac562ff41e2dcfdf829c5a142f1fccd7d"),
			hexutil.Bytes{1, 2, 3, 4, 5},
		)
		require.Error(t, err)

		assert.ErrorContains(
			t,
			err,
			"method is not implemented",
		)
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

		assert.ErrorContains(
			t,
			err,
			"method is not implemented",
		)
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
		)
		require.Error(t, err)

		assert.ErrorContains(
			t,
			err,
			"method is not implemented",
		)
	})
}

func transactionExecutedEvent(
	blockHeight uint64,
	transactionHash string,
	transaction string,
	failed bool,
	transactionType uint8,
	gasConsumed uint64,
	deployedContractAddress string,
	returnedValue string,
	logs string,
) cadence.Event {
	return cadence.Event{
		EventType: cadence.NewEventType(
			stdlib.FlowLocation{},
			"evm.TransactionExecuted",
			[]cadence.Field{
				cadence.NewField("blockHeight", cadence.UInt64Type{}),
				cadence.NewField("transactionHash", cadence.StringType{}),
				cadence.NewField("transaction", cadence.StringType{}),
				cadence.NewField("failed", cadence.BoolType{}),
				cadence.NewField("transactionType", cadence.UInt8Type{}),
				cadence.NewField("gasConsumed", cadence.UInt64Type{}),
				cadence.NewField("deployedContractAddress", cadence.StringType{}),
				cadence.NewField("returnedValue", cadence.StringType{}),
				cadence.NewField("logs", cadence.StringType{}),
			},
			nil,
		),
		Fields: []cadence.Value{
			cadence.NewUInt64(blockHeight),
			cadence.String(transactionHash),
			cadence.String(transaction),
			cadence.NewBool(failed),
			cadence.NewUInt8(transactionType),
			cadence.NewUInt64(gasConsumed),
			cadence.String(deployedContractAddress),
			cadence.String(returnedValue),
			cadence.String(logs),
		},
	}
}

func blockExecutedEvent(
	blockHeight uint64,
	blockHash string,
	totalSupply uint64,
	parentBlockHash string,
	receiptRoot string,
	transactionHashes []string,
) cadence.Event {
	hashes := make([]cadence.Value, len(transactionHashes))
	for i, hash := range transactionHashes {
		hashes[i] = cadence.String(hash)
	}

	return cadence.Event{
		EventType: cadence.NewEventType(
			stdlib.FlowLocation{},
			"evm.BlockExecuted",
			[]cadence.Field{
				cadence.NewField("blockHeight", cadence.UInt64Type{}),
				cadence.NewField("blockHash", cadence.StringType{}),
				cadence.NewField("totalSupply", cadence.UInt64Type{}),
				cadence.NewField("parentBlockHash", cadence.StringType{}),
				cadence.NewField("receiptRoot", cadence.StringType{}),
				cadence.NewField(
					"transactionHashes",
					cadence.NewVariableSizedArrayType(cadence.StringType{}),
				),
			},
			nil,
		),
		Fields: []cadence.Value{
			cadence.NewUInt64(blockHeight),
			cadence.String(blockHash),
			cadence.NewUInt64(totalSupply),
			cadence.String(parentBlockHash),
			cadence.String(receiptRoot),
			cadence.NewArray(hashes).WithType(
				cadence.NewVariableSizedArrayType(cadence.StringType{}),
			),
		},
	}
}
