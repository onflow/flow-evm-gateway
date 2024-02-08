package integration

import (
	"context"
	_ "embed"
	"encoding/hex"
	"github.com/ethereum/go-ethereum/params"
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
	res, err = evmSignAndRun(emu, transferWei, params.TxGas, fundEOAKey, &transferEOAAdress, nil)
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
	res, err = evmSignAndRun(emu, nil, 4700000, eoaKey, nil, deployData)
	require.NoError(t, err)
	require.NoError(t, res.Error)

	time.Sleep(2 * time.Second) // todo change

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

	// todo 5, 6
}
