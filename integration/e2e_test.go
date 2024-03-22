package integration

import (
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/params"
	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/logs"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/cadence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed fixtures/test.bin
var testContractBinary string

//go:embed fixtures/test-abi.json
var testContractABI string

var (
	fundEOAAddress    = common.HexToAddress("FACF71692421039876a5BB4F10EF7A439D8ef61E")
	fundEOARawKey     = "f6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"
	transferEOAAdress = common.HexToAddress("Dac891801DfE8b842E88D0060e1F776256384cB8")
)

// TestIntegration_TransferValue executes interactions in the EVM in the following order
// 1. Create a COA using EVM.createCadenceOwnedAccount()
// 2. Fund that COA - produces direct call EVM transaction events with deposit subtype
// 3. Transfer value from COA to "fund EOA" - produces direct call EVM transaction events with call subtype
// 4. Transfer value from "fund EOA" to another "transfer EOA" - produces block and tx executed EVM event
func TestIntegration_TransferValue(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)
	dbDir := t.TempDir()
	emu := srv.Emulator()

	ctx, cancelIngestion := context.WithCancel(context.Background())
	blocks, receipts, txs, err := startEventIngestionEngine(ctx, dbDir)
	require.NoError(t, err)

	defer func() {
		go func() { srv.Stop() }()
		cancelIngestion()
	}()

	flowAmount, err := cadence.NewUFix64("5.0")
	require.NoError(t, err)
	fundWei := types.NewBalanceFromUFix64(flowAmount)

	// step 1, 2, and 3. - create COA and fund it
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 8) // 6 evm events + 2 cadence events

	flowTransfer, _ := cadence.NewUFix64("1.0")
	transferWei := types.NewBalanceFromUFix64(flowTransfer)
	fundEOAKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	// step 4. - transfer between EOAs
	res, evmID, err := evmSignAndRun(emu, transferWei, params.TxGas, fundEOAKey, 0, &transferEOAAdress, nil)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 2)

	time.Sleep(2 * time.Second) // todo change

	// block 1 comes from EVM.createCadenceOwnedAccount()
	blk, err := blocks.GetByHeight(1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), blk.Height)
	require.Len(t, blk.TransactionHashes, 1)

	// transaction 1 on block 1 comes from EVM.createCadenceOwnedAccount()
	tx, err := txs.Get(blk.TransactionHashes[0])
	require.NoError(t, err)
	from, err := tx.From()
	require.NoError(t, err)
	assert.Equal(t, common.HexToAddress("0x0000000000000000000000020000000000000000"), from)
	assert.Equal(t, uint8(255), tx.Type())
	assert.Equal(t, uint64(0), tx.Nonce())
	assert.Equal(t, big.NewInt(0), tx.Value())
	assert.NotNil(t, tx.To())
	assert.Equal(t, uint64(723_000), tx.Gas())

	coaAddress := tx.To()

	// block 2 comes from calling coa.deposit
	blk, err = blocks.GetByHeight(2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), blk.Height)
	assert.Zero(t, blk.TotalSupply.Cmp(fundWei))
	require.Len(t, blk.TransactionHashes, 1)

	// transaction 1 on block 2 comes from calling coa.deposit
	tx, err = txs.Get(blk.TransactionHashes[0])
	require.NoError(t, err)
	from, err = tx.From()
	require.NoError(t, err)
	assert.Equal(t, common.HexToAddress("0x0000000000000000000000010000000000000000"), from)
	assert.Equal(t, uint8(255), tx.Type())
	assert.Equal(t, uint64(0), tx.Nonce())
	assert.Equal(t, big.NewInt(5000000000000000000), tx.Value())
	assert.Equal(t, coaAddress, tx.To())
	assert.Equal(t, uint64(23_300), tx.Gas())

	// block 3 comes from calling coa.call to transfer to eoa 1
	blk, err = blocks.GetByHeight(3)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), blk.Height)
	assert.Zero(t, blk.TotalSupply.Cmp(fundWei))
	require.Len(t, blk.TransactionHashes, 1)

	// transaction 1 on block 3 comes from calling coa.call to transfer to eoa 1
	tx, err = txs.Get(blk.TransactionHashes[0])
	require.NoError(t, err)
	from, err = tx.From()
	require.NoError(t, err)
	assert.Equal(t, *coaAddress, from)
	assert.Equal(t, uint8(255), tx.Type())
	assert.Equal(t, uint64(1), tx.Nonce())
	assert.Equal(t, big.NewInt(4000000000000000000), tx.Value())
	assert.Equal(t, fundEOAAddress, *tx.To())
	assert.Equal(t, uint64(300_000), tx.Gas())

	// block 4 comes from calling EVM.run to transfer from eoa 1 to eoa 2
	blk, err = blocks.GetByHeight(4)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), blk.Height)
	assert.Zero(t, blk.TotalSupply.Cmp(fundWei))
	require.Len(t, blk.TransactionHashes, 1)

	// transaction 1 on block 4 comes from EVM.run to transfer from eoa 1 to eoa 2
	transferHash := blk.TransactionHashes[0]
	assert.Equal(t, transferHash.String(), evmID.String())

	tx, err = txs.Get(transferHash)
	require.NoError(t, err)
	from, err = tx.From()
	require.NoError(t, err)
	assert.Equal(t, fundEOAAddress, from)
	assert.Equal(t, uint8(0), tx.Type())
	assert.Equal(t, uint64(0), tx.Nonce())
	assert.Zero(t, tx.Value().Cmp(transferWei))
	assert.Equal(t, &transferEOAAdress, tx.To())
	assert.Equal(t, uint64(21_000), tx.Gas())

	rcp, err := receipts.GetByTransactionID(transferHash)
	require.NoError(t, err)
	assert.Equal(t, transferHash, rcp.TxHash)
	assert.Len(t, rcp.Logs, 0)
	assert.Equal(t, blk.Height, rcp.BlockNumber.Uint64())
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	h, err := blk.Hash()
	require.NoError(t, err)
	assert.Equal(t, h, rcp.BlockHash)
}

// TestIntegration_DeployCallContract executes interactions with EVM that produce events
// and makes sure data is correctly indexed. The interactions are:
// 1. Create a COA using EVM.createCadenceOwnedAccount()
// 2. Fund that COA - produces direct call EVM transaction events with deposit subtype
// 3. Transfer value from COA to "fund EOA" - produces direct call EVM transaction events with call subtype
// 4. Deploy a contract using the EOA - produces block and tx executed EVM event
// 5. Execute a function on the new deployed contract that returns a value - produces block and tx executed EVM event
// 6. Execute a function that emits a log multiple times with different values - produces block and tx executed EVM event with logs
//
// The test then proceeds on testing filtering of events
func TestIntegration_DeployCallContract(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)
	dbDir := t.TempDir()

	ctx, cancelIngestion := context.WithCancel(context.Background())
	blocks, receipts, txs, err := startEventIngestionEngine(ctx, dbDir)
	require.NoError(t, err)

	defer func() {
		go func() { srv.Stop() }()
		cancelIngestion()
	}()

	emu := srv.Emulator()

	flowAmount, _ := cadence.NewUFix64("5.0")
	gasLimit := uint64(4700000) // arbitrarily big

	storeContract, err := newContract(testContractBinary, testContractABI)
	require.NoError(t, err)

	// Steps 1, 2 and 3. - create COA and fund it
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 8)

	eoaKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	deployData, err := hex.DecodeString(testContractBinary)
	require.NoError(t, err)

	// Step 4. - deploy contract
	res, _, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 0, nil, deployData)
	require.NoError(t, err)
	require.NoError(t, res.Error)

	time.Sleep(1 * time.Second) // todo change

	latest, err := blocks.LatestEVMHeight()
	require.NoError(t, err)

	// block comes from contract deployment
	blk, err := blocks.GetByHeight(latest)
	require.NoError(t, err)
	require.Len(t, blk.TransactionHashes, 1)

	// check the deployment transaction and receipt
	deployHash := blk.TransactionHashes[0]
	tx, err := txs.Get(deployHash)
	require.NoError(t, err)
	assert.Equal(t, deployData, tx.Data())

	rcp, err := receipts.GetByTransactionID(deployHash)
	require.NoError(t, err)
	assert.Equal(t, deployHash, rcp.TxHash)
	assert.Equal(t, blk.Height, rcp.BlockNumber.Uint64())
	assert.NotEmpty(t, rcp.ContractAddress.Hex())
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, uint64(215324), rcp.GasUsed)
	assert.Len(t, rcp.Logs, 0)

	contractAddress := rcp.ContractAddress

	callRetrieve, err := storeContract.call("retrieve")
	require.NoError(t, err)

	// step 5. - call retrieve function on the contract
	res, _, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 1, &contractAddress, callRetrieve) // todo use nonce tracking to pass in value
	require.NoError(t, err)
	require.NoError(t, res.Error)

	time.Sleep(1 * time.Second)

	// block comes from contract interaction
	latest += 1
	blk, err = blocks.GetByHeight(latest)
	require.NoError(t, err)
	assert.Equal(t, latest, blk.Height)
	require.Len(t, blk.TransactionHashes, 1)

	interactHash := blk.TransactionHashes[0]
	tx, err = txs.Get(interactHash)
	require.NoError(t, err)
	txHash, err := tx.Hash()
	require.NoError(t, err)
	assert.Equal(t, interactHash, txHash)
	assert.Equal(t, callRetrieve, tx.Data())
	assert.Equal(t, contractAddress.Hex(), tx.To().Hex())

	rcp, err = receipts.GetByTransactionID(interactHash)
	require.NoError(t, err)
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, interactHash, rcp.TxHash)
	assert.Equal(t, latest, rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	callStore, err := storeContract.call("store", big.NewInt(1337))
	require.NoError(t, err)

	// step 5 - call store to set the value
	res, _, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 2, &contractAddress, callStore)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	fmt.Println(res.Events)

	time.Sleep(1 * time.Second)

	// block comes from contract store interaction
	latest += 1
	blk, err = blocks.GetByHeight(latest)
	require.NoError(t, err)
	assert.Equal(t, latest, blk.Height)
	require.Len(t, blk.TransactionHashes, 1)

	interactHash = blk.TransactionHashes[0]
	tx, err = txs.Get(interactHash)
	require.NoError(t, err)
	txHash, err = tx.Hash()
	require.NoError(t, err)
	assert.Equal(t, interactHash, txHash)
	assert.Equal(t, callStore, tx.Data())
	assert.Equal(t, contractAddress.Hex(), tx.To().Hex())

	rcp, err = receipts.GetByTransactionID(interactHash)
	require.NoError(t, err)
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, interactHash, rcp.TxHash)
	assert.Equal(t, latest, rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	// step 6 - call sdkEvent emitting function with different values
	heights := make([]uint64, 0)
	for i := 0; i < 4; i++ {
		sumA := big.NewInt(5)
		sumB := big.NewInt(int64(3 + i))
		callSum, err := storeContract.call("sum", sumA, sumB)
		require.NoError(t, err)

		res, _, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, uint64(3+i), &contractAddress, callSum)
		require.NoError(t, err)
		require.NoError(t, res.Error)

		time.Sleep(1 * time.Second)

		// block is produced by above call to the sum that emits sdkEvent
		latest += 1
		blk, err = blocks.GetByHeight(latest)
		require.NoError(t, err)
		require.Len(t, blk.TransactionHashes, 1)
		heights = append(heights, blk.Height)

		sumHash := blk.TransactionHashes[0]
		_, err = txs.Get(sumHash)
		require.NoError(t, err)

		rcp, err = receipts.GetByTransactionID(sumHash)
		require.NoError(t, err)
		assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
		require.Len(t, rcp.Logs, 1)

		// check the sum call sdkEvent
		sumLog := rcp.Logs[0]
		assert.Equal(t, contractAddress.Hex(), sumLog.Address.Hex())
		assert.Equal(t, blk.Height, sumLog.BlockNumber)
		assert.Equal(t, sumHash.Hex(), sumLog.TxHash.Hex())
		assert.Equal(t, fundEOAAddress, common.HexToAddress(sumLog.Topics[1].Hex())) // topic 1 is caller argument
		assert.Equal(t, sumA.Cmp(sumLog.Topics[2].Big()), 0)                         // topic 2 is argument sumA
		assert.Equal(t, sumB.Cmp(sumLog.Topics[3].Big()), 0)                         // topic 3 is argument sumB

		assert.NoError(t, checkSumLogValue(storeContract, sumA, sumB, sumLog.Data))
	}

	// test filtering of events by different filter parameters, we have the following state:
	// block height 9 - sdkEvent topics (eoa, 5, 3)
	// block height 10 - sdkEvent topics (eoa, 5, 4)
	// block height 11 - sdkEvent topics (eoa, 5, 5)
	// block height 12 - sdkEvent topics (eoa, 5, 6)

	// successfully filter by block id with found single match for each block
	for i, height := range heights {
		blk, err = blocks.GetByHeight(height)
		require.NoError(t, err)
		blkID, err := blk.Hash()
		require.NoError(t, err)

		sumA := big.NewInt(5)
		sumB := big.NewInt(int64(3 + i))
		filter := logs.FilterCriteria{
			Addresses: []common.Address{contractAddress},
			Topics: [][]common.Hash{{
				common.HexToHash(fundEOAAddress.Hex()),
				common.BigToHash(sumA),
				common.BigToHash(sumB),
			}},
		}

		matches, err := logs.NewIDFilter(blkID, filter, blocks, receipts).Match()
		require.NoError(t, err)
		require.Len(t, matches, 1)
		match := matches[0]
		assert.NoError(t, checkSumLogValue(storeContract, sumA, sumB, match.Data))
	}

	// invalid filter by block id with wrong topic value
	blk, err = blocks.GetByHeight(heights[0])
	require.NoError(t, err)
	blkID, err := blk.Hash()
	require.NoError(t, err)

	sumA := big.NewInt(5)
	sumB := big.NewInt(99999)
	filter := logs.FilterCriteria{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{{
			common.HexToHash(fundEOAAddress.Hex()),
			common.BigToHash(sumA),
			common.BigToHash(sumB),
		}},
	}

	matches, err := logs.NewIDFilter(blkID, filter, blocks, receipts).Match()
	require.NoError(t, err) // no error for matches not found
	require.Len(t, matches, 0)

	// successfully filter specific log for provided valid range
	sumA = big.NewInt(5)
	sumB = big.NewInt(4)
	filter = logs.FilterCriteria{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{{
			common.HexToHash(fundEOAAddress.Hex()),
			common.BigToHash(sumA),
			common.BigToHash(sumB),
		}},
	}

	first := int64(heights[0])
	last := int64(heights[len(heights)-1])
	f, err := logs.NewRangeFilter(*big.NewInt(first), *big.NewInt(last), filter, receipts)
	require.NoError(t, err)

	matches, err = f.Match()
	require.NoError(t, err)

	require.Len(t, matches, 1)
	assert.NoError(t, checkSumLogValue(storeContract, sumA, sumB, matches[0].Data))

	// successfully filter logs with wildcard as a second topic matching all the logs for provided valid range
	sumA = big.NewInt(5)
	filter = logs.FilterCriteria{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{{
			common.HexToHash(fundEOAAddress.Hex()),
			common.BigToHash(sumA),
		}},
	}

	f, err = logs.NewRangeFilter(*big.NewInt(first), *big.NewInt(last), filter, receipts)
	require.NoError(t, err)

	matches, err = f.Match()
	require.NoError(t, err)

	require.Len(t, matches, 4)
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(3), matches[0].Data))
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(4), matches[1].Data))
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(5), matches[2].Data))
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(6), matches[3].Data))
}

// This test does the same as TestIntegration_DeployCallContract but uses the API for e2e testing.
func TestE2E_API_DeployEvents(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		srv.Stop()
	}()

	emu := srv.Emulator()
	dbDir := t.TempDir()
	gwAcc := emu.ServiceKey()
	gwKey := gwAcc.PrivateKey
	gwAddress := gwAcc.Address

	cfg := &config.Config{
		DatabaseDir:        dbDir,
		AccessNodeGRPCHost: "localhost:3569", // emulator
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		FlowNetworkID:      "flow-emulator",
		EVMNetworkID:       types.FlowEVMTestnetChainID,
		Coinbase:           fundEOAAddress,
		COAAddress:         gwAddress,
		COAKey:             gwKey,
		CreateCOAResource:  false,
		GasPrice:           new(big.Int).SetUint64(0),
	}

	rpcTester := &rpcTest{
		url: fmt.Sprintf("http://%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()

	time.Sleep(500 * time.Millisecond) // some time to startup

	flowAmount, _ := cadence.NewUFix64("5.0")

	// Steps 1, 2 and 3. - create COA and fund it - setup phase
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 8)

	deployData, err := hex.DecodeString(testContractBinary)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// estimate gas for contract deployment
	gasUsed, err := rpcTester.estimateGas(fundEOAAddress, deployData, 200_000)
	require.Error(t, err)
	assert.ErrorContains(t, err, "contract creation code storage out of gas")
	assert.Equal(t, hexutil.Uint64(0), gasUsed)

	gasUsed, err = rpcTester.estimateGas(fundEOAAddress, deployData, 250_000)
	require.NoError(t, err)
	assert.Equal(t, hexutil.Uint64(215_324), gasUsed)

	// check EOA balance
	balance, err := rpcTester.getBalance(fundEOAAddress)
	require.NoError(t, err)
	c, _ := cadence.NewUFix64("4.0")
	assert.Zero(t, balance.ToInt().Cmp(types.NewBalanceFromUFix64(c)))

	// Step 4. - deploy contract
	nonce := uint64(0)
	gasLimit := uint64(4700000) // arbitrarily big
	eoaKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	signed, _, err := evmSign(nil, gasLimit, eoaKey, nonce, nil, deployData)
	nonce++
	hash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)
	require.NotNil(t, hash)

	time.Sleep(1 * time.Second) // todo change

	latestBlockNumber, err := rpcTester.blockNumber()
	require.NoError(t, err)

	// check the eth_getBlockByNumber JSON-RPC endpoint,
	// with `fullTx` option.
	blkRpc, err := rpcTester.getBlock(latestBlockNumber, true)
	require.NoError(t, err)

	require.Len(t, blkRpc.Transactions, 1)
	assert.Equal(t, uintHex(4), blkRpc.Number)

	require.Len(t, blkRpc.FullTransactions(), 1)
	fullTx := blkRpc.FullTransactions()[0]

	assert.Equal(t, blkRpc.Hash, fullTx["blockHash"])
	assert.Equal(t, blkRpc.Number, fullTx["blockNumber"])
	assert.Nil(t, fullTx["to"])

	// check the eth_getBlockByHash JSON-RPC endpoint,
	// with `fullTx` option.
	blkRpc, err = rpcTester.getBlockByHash(blkRpc.Hash, true)
	require.NoError(t, err)

	require.Len(t, blkRpc.Transactions, 1)
	assert.Equal(t, uintHex(4), blkRpc.Number)

	require.Len(t, blkRpc.FullTransactions(), 1)
	fullTx = blkRpc.FullTransactions()[0]

	assert.Equal(t, blkRpc.Hash, fullTx["blockHash"])
	assert.Equal(t, blkRpc.Number, fullTx["blockNumber"])
	assert.Nil(t, fullTx["to"])

	// check the deployment transaction and receipt
	deployHash := blkRpc.TransactionHashes()[0]
	require.Equal(t, hash.String(), deployHash)

	txRpc, err := rpcTester.getTx(deployHash)
	require.NoError(t, err)

	assert.Equal(t, deployHash, txRpc.Hash.String())
	assert.Equal(t, deployData, []byte(txRpc.Input))

	rcp, err := rpcTester.getReceipt(deployHash)
	require.NoError(t, err)

	assert.Equal(t, deployHash, rcp.TxHash.String())
	assert.Equal(t, uint64(4), rcp.BlockNumber.Uint64())
	assert.NotEmpty(t, rcp.ContractAddress.Hex())
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, uint64(215324), rcp.GasUsed)
	assert.Len(t, rcp.Logs, 0)

	contractAddress := rcp.ContractAddress

	// Check code retrieval
	code, err := rpcTester.getCode(contractAddress)
	require.NoError(t, err)
	// The deployed byte code is a subset of the byte code provided in
	// contract deployment tx.
	assert.Contains(t, hex.EncodeToString(deployData), hex.EncodeToString(code))

	storeContract, err := newContract(testContractBinary, testContractABI)
	require.NoError(t, err)

	callRetrieve, err := storeContract.call("retrieve")
	require.NoError(t, err)

	// step 5. - call retrieve function on the contract
	signed, signedHash, err := evmSign(nil, gasLimit, eoaKey, nonce, &contractAddress, callRetrieve)
	nonce++
	require.NoError(t, err)

	hash, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)
	assert.NotNil(t, hash)
	assert.Equal(t, signedHash.String(), hash.String())

	time.Sleep(1 * time.Second)

	// check if the sender account nonce has been indexed as increased
	eoaNonce, err := rpcTester.getNonce(fundEOAAddress)
	require.NoError(t, err)
	assert.Equal(t, nonce, eoaNonce)

	// block 5 comes from contract interaction
	latestBlockNumber, err = rpcTester.blockNumber()
	require.NoError(t, err)

	blkRpc, err = rpcTester.getBlock(latestBlockNumber, false)
	require.NoError(t, err)
	assert.Equal(t, uintHex(5), blkRpc.Number)
	require.Len(t, blkRpc.Transactions, 1)

	interactHash := blkRpc.TransactionHashes()[0]
	assert.Equal(t, signedHash.String(), interactHash)

	txRpc, err = rpcTester.getTx(interactHash)
	require.NoError(t, err)
	assert.Equal(t, interactHash, txRpc.Hash.String())
	assert.Equal(t, callRetrieve, []byte(txRpc.Input))
	assert.Equal(t, contractAddress.Hex(), txRpc.To.Hex())

	rcp, err = rpcTester.getReceipt(interactHash)
	require.NoError(t, err)
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, interactHash, rcp.TxHash.String())
	assert.Equal(t, uint64(5), rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	callStore, err := storeContract.call("store", big.NewInt(1337))
	require.NoError(t, err)

	// step 5 - call store to set the value
	signed, signedHash, err = evmSign(nil, gasLimit, eoaKey, nonce, &contractAddress, callStore)
	nonce++
	require.NoError(t, err)

	hash, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)
	assert.NotNil(t, hash)
	assert.Equal(t, signedHash.String(), hash.String())

	time.Sleep(1 * time.Second)

	// perform `eth_call` to read the stored value
	storedValue, err := rpcTester.call(contractAddress, callRetrieve)
	require.NoError(t, err)
	assert.Equal(
		t,
		"0000000000000000000000000000000000000000000000000000000000000539", // 1337 in ABI encoding
		hex.EncodeToString(storedValue),
	)

	// check if the sender account nonce has been indexed as increased
	eoaNonce, err = rpcTester.getNonce(fundEOAAddress)
	require.NoError(t, err)
	assert.Equal(t, nonce, eoaNonce)

	// block 6 comes from contract store interaction
	latestBlockNumber, err = rpcTester.blockNumber()
	require.NoError(t, err)

	blkRpc, err = rpcTester.getBlock(latestBlockNumber, false)
	require.NoError(t, err)
	assert.Equal(t, uintHex(6), blkRpc.Number)
	require.Len(t, blkRpc.Transactions, 1)

	interactHash = blkRpc.TransactionHashes()[0]
	assert.Equal(t, signedHash.String(), interactHash)

	txRpc, err = rpcTester.getTx(interactHash)
	require.NoError(t, err)
	assert.Equal(t, interactHash, txRpc.Hash.String())
	assert.Equal(t, callStore, []byte(txRpc.Input))
	assert.Equal(t, contractAddress.Hex(), txRpc.To.Hex())

	rcp, err = rpcTester.getReceipt(interactHash)
	require.NoError(t, err)
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, interactHash, rcp.TxHash.String())
	assert.Equal(t, uint64(6), rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	// step 6 - call sdkEvent emitting function with different values
	for i := 0; i < 4; i++ {
		sumA := big.NewInt(5)
		sumB := big.NewInt(int64(3 + i))
		callSum, err := storeContract.call("sum", sumA, sumB)
		require.NoError(t, err)

		signed, signedHash, err = evmSign(nil, gasLimit, eoaKey, nonce, &contractAddress, callSum)
		nonce++
		require.NoError(t, err)

		hash, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)
		assert.NotNil(t, hash)
		assert.Equal(t, signedHash.String(), hash.String())

		time.Sleep(1 * time.Second)

		// block is produced by above call to the sum that emits sdkEvent
		blkRpc, err = rpcTester.getBlock(latestBlockNumber+1+uint64(i), false)
		require.NoError(t, err)
		require.Len(t, blkRpc.Transactions, 1)

		sumHash := blkRpc.TransactionHashes()[0]
		assert.Equal(t, signedHash.String(), sumHash)

		_, err = rpcTester.getTx(sumHash)
		require.NoError(t, err)

		rcp, err = rpcTester.getReceipt(sumHash)
		require.NoError(t, err)
		assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
		require.Len(t, rcp.Logs, 1)

		// check the sum call sdkEvent
		sumLog := rcp.Logs[0]
		assert.Equal(t, contractAddress.Hex(), sumLog.Address.Hex())
		assert.Equal(t, fundEOAAddress, common.HexToAddress(sumLog.Topics[1].Hex())) // topic 1 is caller argument
		assert.Equal(t, sumA.Cmp(sumLog.Topics[2].Big()), 0)                         // topic 2 is argument sumA
		assert.Equal(t, sumB.Cmp(sumLog.Topics[3].Big()), 0)                         // topic 3 is argument sumB

		assert.NoError(t, checkSumLogValue(storeContract, sumA, sumB, sumLog.Data))
	}

	// test filtering of events by different filter parameters, we have the following state:
	// block height 7 - sdkEvent topics (eoa, 5, 3)
	// block height 8 - sdkEvent topics (eoa, 5, 4)
	// block height 9 - sdkEvent topics (eoa, 5, 5)
	// block height 10 - sdkEvent topics (eoa, 5, 6)

	// successfully filter by block id with found single match for each block
	for i := 0; i < 4; i++ {
		blkRpc, err = rpcTester.getBlock(latestBlockNumber+1+uint64(i), false)
		require.NoError(t, err)

		blkID := common.HexToHash(blkRpc.Hash)

		sumA := big.NewInt(5)
		sumB := big.NewInt(int64(3 + i))
		filter := logs.FilterCriteria{
			Addresses: []common.Address{contractAddress},
			Topics: [][]common.Hash{{
				common.HexToHash(fundEOAAddress.Hex()),
				common.BigToHash(sumA),
				common.BigToHash(sumB),
			}},
		}

		matches, err := rpcTester.getLogs(&blkID, nil, nil, &filter)

		require.NoError(t, err)
		require.Len(t, matches, 1)
		match := matches[0]
		assert.NoError(t, checkSumLogValue(storeContract, sumA, sumB, match.Data))
	}

	// invalid filter by block id with wrong topic value
	blkRpc, err = rpcTester.getBlock(latestBlockNumber+1, false)
	require.NoError(t, err)
	blkID := common.HexToHash(blkRpc.Hash)
	require.NoError(t, err)

	sumA := big.NewInt(5)
	sumB := big.NewInt(99999)
	filter := logs.FilterCriteria{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{{
			common.HexToHash(fundEOAAddress.Hex()),
			common.BigToHash(sumA),
			common.BigToHash(sumB),
		}},
	}

	matches, err := rpcTester.getLogs(&blkID, nil, nil, &filter)
	require.NoError(t, err) // no error for matches not found
	require.Len(t, matches, 0)

	// successfully filter specific log for provided valid range
	sumA = big.NewInt(5)
	sumB = big.NewInt(4)
	filter = logs.FilterCriteria{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{{
			common.HexToHash(fundEOAAddress.Hex()),
			common.BigToHash(sumA),
			common.BigToHash(sumB),
		}},
	}

	matches, err = rpcTester.getLogs(nil, big.NewInt(7), big.NewInt(10), &filter)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NoError(t, checkSumLogValue(storeContract, sumA, sumB, matches[0].Data))

	// successfully filter logs with wildcard as a second topic matching all the logs for provided valid range
	sumA = big.NewInt(5)
	filter = logs.FilterCriteria{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{{
			common.HexToHash(fundEOAAddress.Hex()),
			common.BigToHash(sumA),
		}},
	}

	matches, err = rpcTester.getLogs(nil, big.NewInt(7), big.NewInt(10), &filter)
	require.NoError(t, err)
	require.Len(t, matches, 4)
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(3), matches[0].Data))
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(4), matches[1].Data))
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(5), matches[2].Data))
	assert.NoError(t, checkSumLogValue(storeContract, sumA, big.NewInt(6), matches[3].Data))
}

// TestE2E_ConcurrentTransactionSubmission test submits multiple transactions concurrently
// and makes sure the transactions were submitted successfully. This is using the
// key-rotation signer that can handle multiple concurrent transactions.
func TestE2E_ConcurrentTransactionSubmission(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)
	emu := srv.Emulator()
	dbDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		srv.Stop()
	}()

	gwAcc := emu.ServiceKey()
	gwAddress := gwAcc.Address
	host := "localhost:3569" // emulator

	client, err := grpc.NewClient(host)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond) // some time to startup

	// create new account with keys used for key-rotation
	keyCount := 5
	createdAddr, keys, err := bootstrap.CreateMultiKeyAccount(
		client,
		keyCount,
		gwAddress,
		"0xee82856bf20e2aa6",
		"0x0ae53cb6e3f42a79",
		gwAcc.PrivateKey,
	)
	require.NoError(t, err)

	cfg := &config.Config{
		DatabaseDir:        dbDir,
		AccessNodeGRPCHost: host,
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		EVMNetworkID:       types.FlowEVMTestnetChainID,
		FlowNetworkID:      "flow-emulator",
		Coinbase:           fundEOAAddress,
		COAAddress:         *createdAddr,
		COAKeys:            keys,
		CreateCOAResource:  true,
		GasPrice:           new(big.Int).SetUint64(0),
	}

	rpcTester := &rpcTest{
		url: fmt.Sprintf("http://%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()
	time.Sleep(500 * time.Millisecond) // some time to startup

	flowAmount, _ := cadence.NewUFix64("5.0")
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 8)

	eoaKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")

	// disable auto-mine so we can control delays
	emu.DisableAutoMine()

	totalTxs := keyCount*5 + 3
	hashes := make([]common.Hash, totalTxs)
	for i := 0; i < totalTxs; i++ {
		nonce := uint64(i)
		signed, signedHash, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
		nonce++
		require.NoError(t, err)

		hash, err := rpcTester.sendRawTx(signed)
		require.NoError(t, err)
		assert.NotNil(t, hash)
		assert.Equal(t, signedHash.String(), hash.String())
		hashes[i] = signedHash

		// execute commit block every 3 blocks so we make sure we should have conflicts with seq numbers if keys not rotated
		if i%3 == 0 {
			_, _, _ = emu.ExecuteAndCommitBlock()
		}
	}

	time.Sleep(5 * time.Second) // wait for all txs to be executed

	for _, h := range hashes {
		rcp, err := rpcTester.getReceipt(h.String())
		require.NoError(t, err)
		assert.Equal(t, uint64(1), rcp.Status)
	}
}

// TestE2E_Streaming is a function used to test end-to-end streaming of data.
func TestE2E_Streaming(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		srv.Stop()
	}()

	emu := srv.Emulator()
	dbDir := t.TempDir()
	gwAcc := emu.ServiceKey()
	gwKey := gwAcc.PrivateKey
	gwAddress := gwAcc.Address

	cfg := &config.Config{
		DatabaseDir:        dbDir,
		AccessNodeGRPCHost: "localhost:3569", // emulator
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		FlowNetworkID:      "flow-emulator",
		EVMNetworkID:       types.FlowEVMTestnetChainID,
		Coinbase:           fundEOAAddress,
		COAAddress:         gwAddress,
		COAKey:             gwKey,
		CreateCOAResource:  true,
		GasPrice:           new(big.Int).SetUint64(1),
		LogWriter:          os.Stdout,
		LogLevel:           zerolog.DebugLevel,
		StreamLimit:        5,
		StreamTimeout:      5 * time.Second,
	}

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()

	time.Sleep(500 * time.Millisecond) // some time to startup

	wsWrite, wsRead, err := rpcTester.wsConnect()
	require.NoError(t, err)
	err = wsWrite(newHeadsRequest())
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		flowTransfer, _ := cadence.NewUFix64("1.0")
		transferWei := types.NewBalanceFromUFix64(flowTransfer)
		fundEOAKey, err := crypto.HexToECDSA(fundEOARawKey)
		require.NoError(t, err)
		_, _, err = evmSignAndRun(emu, transferWei, params.TxGas, fundEOAKey, uint64(i), &transferEOAAdress, nil)
		require.NoError(t, err)
	}

	currentHeight := 2 // first two blocks are used for evm setup events
	var subscriptionID string
	for i := 0; i < 5; i++ {
		event, err := wsRead()
		require.NoError(t, err)

		var msg streamMsg
		require.NoError(t, json.Unmarshal(event, &msg))
		if msg.Params.Result["number"] == nil {
			continue // skip the first event that only returns the id
		}

		// this makes sure we receive the events in correct order
		h, err := hexutil.DecodeUint64(msg.Params.Result["number"].(string))
		require.NoError(t, err)
		assert.Equal(t, currentHeight, int(h))
		currentHeight++
		subscriptionID = msg.Params.Subscription
	}

	require.NotEmpty(t, subscriptionID)
	err = wsWrite(unsubscribeRequest(subscriptionID))
	require.NoError(t, err)

	// successfully unsubscribed
	res, err := wsRead()
	require.NoError(t, err)

	var u map[string]any
	require.NoError(t, json.Unmarshal(res, &u))
	require.True(t, u["result"].(bool))
}

// checkSumLogValue makes sure the match is correct by checking sum value
func checkSumLogValue(c *contract, a *big.Int, b *big.Int, data []byte) error {
	event, err := c.value("Calculated", data)
	if err != nil {
		return err
	}

	returnValue := event[0].(*big.Int)

	correctSum := new(big.Int).Add(a, b)
	if correctSum.Cmp(returnValue) != 0 {
		return fmt.Errorf("log value not correct, should be %d but is %d", correctSum.Int64(), returnValue.Int64())
	}

	return nil
}
