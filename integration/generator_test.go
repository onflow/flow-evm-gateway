package integration

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"testing"
	"time"

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
// 3. Transfer value from COA to "fund EOA" - produces evm event with direct call with call subtype txs
// 4. Transfer value from "fund EOA" to another "transfer EOA" - produces evm transaction event
func TestIntegration_TransferValue(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	ctx, cancelIngestion := context.WithCancel(context.Background())
	blocks, receipts, txs, err := startEventIngestionEngine(ctx)
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
	assert.Len(t, res.Events, 6) // 4 evm events + 2 cadence events

	transferWei := flowToWei(1)
	fundEOAKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	// step 4. - transfer between EOAs
	res, err = evmSignAndRun(emu, transferWei, params.TxGas, fundEOAKey, 0, &transferEOAAdress, nil)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Len(t, res.Events, 2)

	time.Sleep(2 * time.Second) // todo change

	// block 1 comes from calling evm.deposit
	blk, err := blocks.GetByHeight(1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), blk.Height)

	assert.Equal(t, fundWei.Cmp(blk.TotalSupply), 0)
	require.Len(t, blk.TransactionHashes, 1)

	// block 2 comes from calling evm.call to transfer to eoa 1
	blk, err = blocks.GetByHeight(2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), blk.Height)
	assert.Equal(t, fundWei.Cmp(blk.TotalSupply), 0)
	require.Len(t, blk.TransactionHashes, 1)

	// block 3 comes from calling evm.call to transfer from eoa 1 to eoa 2
	blk, err = blocks.GetByHeight(3)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), blk.Height)
	assert.Equal(t, fundWei.Cmp(blk.TotalSupply), 0)
	require.Len(t, blk.TransactionHashes, 1)

	// transaction 1 comes from evm.call to transfer from eoa 1 to eoa 2
	transferHash := blk.TransactionHashes[0]
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
	/* todo add block hash in evm core event
	h, err := blk.Hash()
	require.NoError(t, err)
	assert.Equal(t, h, rcp.BlockHash)
	*/
}

// TestIntegration_DeployCallContract executes interactions with EVM that produce events
// and makes sure data is correctly indexed. The interactions are:
// 1. Create an COA using the createBridgedAccount - doesn't produce evm events
// 2. Fund that COA - produces evm events with direct call with deposit subtype txs
// 3. Transfer value from COA to "fund EOA" - produces evm event with direct call with call subtype txs
// 4. Deploy a contract using the EOA - produces block event as well as transaction executed event
// 5. Execute a function on the new deployed contract that returns a value - produces block and tx executed event
// 6. Execute a function that emits a log multiple times with different values - produces block and tx executed event with logs
func TestIntegration_DeployCallContract(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	ctx, cancelIngestion := context.WithCancel(context.Background())
	blocks, receipts, txs, err := startEventIngestionEngine(ctx)
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
	assert.Len(t, res.Events, 6) // 4 evm events + 2 cadence events

	eoaKey, err := crypto.HexToECDSA(fundEOARawKey)
	require.NoError(t, err)

	deployData, err := hex.DecodeString(testContractBinary)
	require.NoError(t, err)

	// Step 4. - deploy contract
	res, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 0, nil, deployData)
	require.NoError(t, err)
	require.NoError(t, res.Error)

	time.Sleep(1 * time.Second) // todo change

	// block 3 comes from contract deployment
	blk, err := blocks.GetByHeight(3)
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
	res, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 1, &contractAddress, callRetrieve) // todo use nonce tracking to pass in value
	require.NoError(t, err)
	require.NoError(t, res.Error)

	time.Sleep(1 * time.Second)

	// block 4 comes from contract interaction
	blk, err = blocks.GetByHeight(4)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), blk.Height)
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
	assert.Equal(t, uint64(4), rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	callStore, err := storeContract.call("store", big.NewInt(1337))
	require.NoError(t, err)

	// step 5 - call store to set the value
	res, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 2, &contractAddress, callStore)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	fmt.Println(res.Events)

	time.Sleep(1 * time.Second)

	// block 5 comes from contract store interaction
	blk, err = blocks.GetByHeight(5)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), blk.Height)
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
	assert.Equal(t, uint64(5), rcp.BlockNumber.Uint64())
	assert.Len(t, rcp.Logs, 0)

	// step 6 - call event emitting function with different values
	callSum, err := storeContract.call("sum", big.NewInt(5), big.NewInt(3))
	require.NoError(t, err)

	res, err = evmSignAndRun(emu, nil, gasLimit, eoaKey, 3, &contractAddress, callSum)
	require.NoError(t, err)
	require.NoError(t, res.Error)

	time.Sleep(1 * time.Second)

	blk, err = blocks.GetByHeight(6)
	require.NoError(t, err)
	require.Len(t, blk.TransactionHashes, 1)

	sumHash := blk.TransactionHashes[0]
	tx, err = txs.Get(sumHash)
	require.NoError(t, err)

	rcp, err = receipts.GetByTransactionID(sumHash)
	require.NoError(t, err)
	assert.Equal(t, gethTypes.ReceiptStatusSuccessful, rcp.Status)
	require.Len(t, rcp.Logs, 1)

	sumLog := rcp.Logs[0]
	assert.Equal(t, contractAddress.Hex(), sumLog.Address.Hex())
	assert.Equal(t, blk.Height, sumLog.BlockNumber)
	assert.Equal(t, sumHash.Hex(), sumLog.TxHash.Hex())

	event, err := storeContract.value("Calculated", sumLog.Data)
	require.NoError(t, err)
	fmt.Println("event", event)
}
