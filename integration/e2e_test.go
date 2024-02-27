package integration

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/params"
	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-go/fvm/evm/emulator"

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
// 1. Create an COA using the createBridgedAccount - doesn't produce evm events
// 2. Fund that COA - produces evm events with direct call with deposit subtype txs
// 3. Transfer value from COA to "fund EOA" - produces evm sdkEvent with direct call with call subtype txs
// 4. Transfer value from "fund EOA" to another "transfer EOA" - produces evm transaction sdkEvent
func TestIntegration_TransferValue(t *testing.T) {
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

	fundAmount := int64(5)
	flowAmount, _ := cadence.NewUFix64("5.0")
	fundWei := flowToWei(fundAmount)

	// step 1, 2, and 3. - create COA and fund it
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 9) // 7 evm events + 2 cadence events

	transferWei := flowToWei(1)
	fundEOAKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	// step 4. - transfer between EOAs
	res, evmID, err := evmSignAndRun(emu, transferWei, params.TxGas, fundEOAKey, 0, &transferEOAAdress, nil)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 5)

	time.Sleep(2 * time.Second) // todo change

	// block 2 comes from calling evm.deposit
	blk, err := blocks.GetByHeight(2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), blk.Height)

	assert.Equal(t, fundWei.Cmp(blk.TotalSupply), 0)
	require.Len(t, blk.TransactionHashes, 1)

	// block 3 comes from calling evm.call to transfer to eoa 1
	blk, err = blocks.GetByHeight(3)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), blk.Height)
	assert.Equal(t, fundWei.Cmp(blk.TotalSupply), 0)
	require.Len(t, blk.TransactionHashes, 1)

	// block 5 comes from calling evm.call to transfer from eoa 1 to eoa 2
	blk, err = blocks.GetByHeight(5)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), blk.Height)
	assert.Equal(t, fundWei.Cmp(blk.TotalSupply), 0)
	require.Len(t, blk.TransactionHashes, 1)

	// transaction 1 comes from evm.call to transfer from eoa 1 to eoa 2
	transferHash := blk.TransactionHashes[0]
	assert.Equal(t, transferHash.String(), evmID.String())

	tx, err := txs.Get(transferHash)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), tx.Nonce())
	assert.Equal(t, transferWei, tx.Value())
	assert.Equal(t, &transferEOAAdress, tx.To())
	assert.Equal(t, uint64(21_000), tx.Gas())

	rcp, err := receipts.GetByTransactionID(transferHash)
	require.NoError(t, err)
	assert.Equal(t, transferHash, rcp.TxHash)
	assert.Len(t, rcp.Logs, 0)
	assert.Equal(t, blk.Height, rcp.BlockNumber.Uint64())
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	/* todo add block hash in evm core sdkEvent
	h, err := blk.Hash()
	require.NoError(t, err)
	assert.Equal(t, h, rcp.BlockHash)
	*/
}

// TestIntegration_DeployCallContract executes interactions with EVM that produce events
// and makes sure data is correctly indexed. The interactions are:
// 1. Create an COA using the createBridgedAccount - doesn't produce evm events
// 2. Fund that COA - produces evm events with direct call with deposit subtype txs
// 3. Transfer value from COA to "fund EOA" - produces evm sdkEvent with direct call with call subtype txs
// 4. Deploy a contract using the EOA - produces block sdkEvent as well as transaction executed sdkEvent
// 5. Execute a function on the new deployed contract that returns a value - produces block and tx executed sdkEvent
// 6. Execute a function that emits a log multiple times with different values - produces block and tx executed sdkEvent with logs
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
	assert.Len(t, res.Events, 9)

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
	// we have to increment by two always, because each evmSignAndRun produces
	// two blocks, one for creating COA and other for running tx
	latest += 2
	blk, err = blocks.GetByHeight(latest)
	require.NoError(t, err)
	assert.Equal(t, latest, blk.Height)
	require.Len(t, blk.TransactionHashes, 1)

	interactHash := blk.TransactionHashes[0]
	tx, err = txs.Get(interactHash)
	require.NoError(t, err)
	assert.Equal(t, interactHash, tx.Hash())
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
	latest += 2
	blk, err = blocks.GetByHeight(latest)
	require.NoError(t, err)
	assert.Equal(t, latest, blk.Height)
	require.Len(t, blk.TransactionHashes, 1)

	interactHash = blk.TransactionHashes[0]
	tx, err = txs.Get(interactHash)
	require.NoError(t, err)
	assert.Equal(t, interactHash, tx.Hash())
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
		latest += 2
		blk, err = blocks.GetByHeight(latest)
		require.NoError(t, err)
		require.Len(t, blk.TransactionHashes, 1)
		heights = append(heights, blk.Height)

		sumHash := blk.TransactionHashes[0]
		tx, err = txs.Get(sumHash)
		require.NoError(t, err)

		rcp, err = receipts.GetByTransactionID(sumHash)
		require.NoError(t, err)
		assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
		require.Len(t, rcp.Logs, 1)

		// check the sum call sdkEvent
		sumLog := rcp.Logs[0]
		assert.Equal(t, contractAddress.Hex(), sumLog.Address.Hex())
		// todo https://github.com/onflow/flow-go/blob/b3279863c7787d112128188a243905a43ec1654a/fvm/evm/emulator/emulator.go#L397
		//assert.Equal(t, blk.Height, sumLog.BlockNumber)
		//assert.Equal(t, sumHash.Hex(), sumLog.TxHash.Hex())
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
	emu := srv.Emulator()
	dbDir := t.TempDir()

	gwAcc := emu.ServiceKey()
	gwKey := gwAcc.PrivateKey
	gwAddress := gwAcc.Address

	cfg := &config.Config{
		DatabaseDir:        dbDir,
		AccessNodeGRPCHost: "localhost:3569", // emulator
		RPCPort:            3001,
		RPCHost:            "127.0.0.1",
		InitCadenceHeight:  0,
		FlowNetworkID:      "emulator",
		EVMNetworkID:       emulator.FlowEVMTestnetChainID,
		Coinbase:           fundEOAAddress,
		COAAddress:         gwAddress,
		COAKey:             gwKey,
		CreateCOAResource:  true,
		GasPrice:           new(big.Int).SetUint64(1),
	}

	rpcTester := &rpcTest{
		url: fmt.Sprintf("http://%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	go func() {
		err = bootstrap.Start(cfg)
		require.NoError(t, err)
	}()
	time.Sleep(500 * time.Millisecond) // some time to startup

	flowAmount, _ := cadence.NewUFix64("5.0")
	gasLimit := uint64(4700000) // arbitrarily big

	storeContract, err := newContract(testContractBinary, testContractABI)
	require.NoError(t, err)

	// Steps 1, 2 and 3. - create COA and fund it - setup phase
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 9)

	eoaKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	deployData, err := hex.DecodeString(testContractBinary)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// estimate gas
	gasUsed, err := rpcTester.estimateGas(fundEOAAddress, deployData, 200_000)
	require.Error(t, err)
	assert.ErrorContains(t, err, "contract creation code storage out of gas")
	assert.Equal(t, hexutil.Uint64(0), gasUsed)

	gasUsed, err = rpcTester.estimateGas(fundEOAAddress, deployData, 250_000)
	require.NoError(t, err)
	assert.Equal(t, hexutil.Uint64(215_324), gasUsed)

	// check balance
	balance, err := rpcTester.getBalance(fundEOAAddress)
	require.NoError(t, err)
	assert.Equal(t, new(big.Int).Mul(big.NewInt(4), toWei), balance.ToInt())

	// Step 4. - deploy contract
	nonce := uint64(0)
	signed, _, err := evmSign(nil, gasLimit, eoaKey, nonce, nil, deployData)
	nonce++
	hash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)
	require.NotNil(t, hash)

	time.Sleep(1 * time.Second) // todo change

	// todo improve tests to use get latest block request instead of manually tracking block heights
	// block 6 comes from contract deployment, all blocks before are from creating COA funded account
	blkRpc, err := rpcTester.getBlock(6)
	require.NoError(t, err)

	assert.Len(t, blkRpc.Transactions, 1)
	assert.Equal(t, uintHex(6), blkRpc.Number)

	// check the deployment transaction and receipt
	deployHash := blkRpc.Transactions[0]

	// todo require.Equal(t, hash.String(), deployHash)
	txRpc, err := rpcTester.getTx(deployHash)
	require.NoError(t, err)

	assert.Equal(t, deployHash, txRpc.Hash.String())
	assert.Equal(t, deployData, []byte(txRpc.Input))

	rcp, err := rpcTester.getReceipt(deployHash)
	require.NoError(t, err)

	assert.Equal(t, deployHash, rcp.TxHash.String())
	assert.Equal(t, uint64(6), rcp.BlockNumber.Uint64())
	assert.NotEmpty(t, rcp.ContractAddress.Hex())
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	assert.Equal(t, uint64(215324), rcp.GasUsed)
	assert.Len(t, rcp.Logs, 0)

	contractAddress := rcp.ContractAddress

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
	blkRpc, err = rpcTester.getBlock(7)
	require.NoError(t, err)
	assert.Equal(t, uintHex(7), blkRpc.Number)
	require.Len(t, blkRpc.Transactions, 1)

	interactHash := blkRpc.Transactions[0]
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
	assert.Equal(t, uint64(7), rcp.BlockNumber.Uint64())
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

	// check if the sender account nonce has been indexed as increased
	eoaNonce, err = rpcTester.getNonce(fundEOAAddress)
	require.NoError(t, err)
	assert.Equal(t, nonce, eoaNonce)

	// block comes from contract store interaction
	blkRpc, err = rpcTester.getBlock(8)
	require.NoError(t, err)
	assert.Equal(t, uintHex(8), blkRpc.Number)
	require.Len(t, blkRpc.Transactions, 1)

	interactHash = blkRpc.Transactions[0]
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
	assert.Equal(t, uint64(8), rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	callStore, err = storeContract.call("store", big.NewInt(1337))
	require.NoError(t, err)

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
		blkRpc, err = rpcTester.getBlock(uint64(9 + i))
		require.NoError(t, err)
		require.Len(t, blkRpc.Transactions, 1)

		sumHash := blkRpc.Transactions[0]
		assert.Equal(t, signedHash.String(), sumHash)

		txRpc, err = rpcTester.getTx(sumHash)
		require.NoError(t, err)

		rcp, err = rpcTester.getReceipt(sumHash)
		require.NoError(t, err)
		assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
		require.Len(t, rcp.Logs, 1)

		// check the sum call sdkEvent
		sumLog := rcp.Logs[0]
		assert.Equal(t, contractAddress.Hex(), sumLog.Address.Hex())
		// todo https://github.com/onflow/flow-go/blob/b3279863c7787d112128188a243905a43ec1654a/fvm/evm/emulator/emulator.go#L397
		//assert.Equal(t, blk.Height, sumLog.BlockNumber)
		//assert.Equal(t, sumHash.Hex(), sumLog.TxHash.Hex())
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
	for i := 0; i < 4; i++ {
		blkRpc, err = rpcTester.getBlock(uint64(9 + i))
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
	blkRpc, err = rpcTester.getBlock(9)
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

	matches, err = rpcTester.getLogs(nil, big.NewInt(9), big.NewInt(12), &filter)
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

	matches, err = rpcTester.getLogs(nil, big.NewInt(9), big.NewInt(12), &filter)
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
		RPCPort:            3001,
		RPCHost:            "127.0.0.1",
		InitCadenceHeight:  0,
		EVMNetworkID:       emulator.FlowEVMTestnetChainID,
		FlowNetworkID:      "emulator",
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
		err = bootstrap.Start(cfg)
		require.NoError(t, err)
	}()
	time.Sleep(500 * time.Millisecond) // some time to startup

	flowAmount, _ := cadence.NewUFix64("5.0")
	res, err := fundEOA(emu, flowAmount, fundEOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 9)

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
