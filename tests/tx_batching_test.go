package tests

import (
	"context"
	"fmt"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/cadence"
	jsonCdc "github.com/onflow/cadence/encoding/json"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

// Test_BatchTxPool_OutOfOrderBurst is the DFNS regression scenario:
// a burst of transactions from a single EOA, arriving out of nonce order,
// must all be executed exactly once - no drops, no duplicates.
func Test_BatchTxPool_OutOfOrderBurst(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	totalTxs := 10
	transferAmount := int64(50_000)

	// Sign 10 transfers with nonces 0..9.
	signedTxs := make([][]byte, totalTxs)
	for nonce := range totalTxs {
		signed, _, err := evmSign(
			big.NewInt(transferAmount),
			205_000,
			privateKey,
			uint64(nonce),
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)
		signedTxs[nonce] = signed
	}

	startBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	// Send them concurrently in shuffled nonce order.
	// All sends must be accepted by the pool without errors.
	shuffledNonces := []int{6, 2, 8, 0, 1, 9, 3, 5, 4, 7}
	g := errgroup.Group{}
	for _, nonce := range shuffledNonces {
		signed := signedTxs[nonce]
		g.Go(func() error {
			_, err := rpcTester.sendRawTx(signed)
			return err
		})
	}

	err = g.Wait()
	require.NoError(t, err)

	expectedBalance := int64(totalTxs) * transferAmount

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(expectedBalance)) == 0
	}, time.Second*30, time.Second*1, "all transactions were not executed")

	endBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	blockEvents, err := emu.GetEventsForHeightRange(
		"A.f8d6e0586b0a20c7.EVM.TransactionExecuted",
		startBlock.Height+1,
		endBlock.Height,
	)
	require.NoError(t, err)

	totalEVMEvents := 0
	for _, blockEvent := range blockEvents {
		totalEVMEvents += len(blockEvent.Events)
	}

	// Exactly 10 EVM transactions executed: no drops, no duplicates.
	assert.Equal(t, totalTxs, totalEVMEvents)
}

// Test_BatchTxPool_OutOfOrderBurstSubmissionSpacingPreserved asserts that
// a burst of out-of-order transactions with arbitrary spacing, will always
// preserve the necessary spacing between consecutive submissions
func Test_BatchTxPool_OutOfOrderBurstSubmissionSpacingPreserved(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	privateKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	totalTxs := 10
	transferAmount := int64(50_000)

	// Sign 10 transfers with nonces 0..9.
	signedTxs := make([][]byte, totalTxs)
	for nonce := range totalTxs {
		signed, _, err := evmSign(
			big.NewInt(transferAmount),
			205_000,
			privateKey,
			uint64(nonce),
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)
		signedTxs[nonce] = signed
	}

	startBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	// Send them concurrently in shuffled nonce order.
	// All sends must be accepted by the pool without errors.
	shuffledNonces := []int{6, 2, 8, 0, 1, 9, 3, 5, 4, 7}
	g := errgroup.Group{}
	for _, nonce := range shuffledNonces {
		signed := signedTxs[nonce]
		g.Go(func() error {
			// Add a bit of random waiting time, to simulate spacing.
			waitTime := rand.IntN(5) * 1000
			time.Sleep(time.Duration(waitTime) * time.Millisecond)
			_, err := rpcTester.sendRawTx(signed)
			return err
		})
	}

	err = g.Wait()
	require.NoError(t, err)

	expectedBalance := int64(totalTxs) * transferAmount

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(expectedBalance)) == 0
	}, time.Second*30, time.Second*1, "all transactions were not executed")

	endBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	blockEvents, err := emu.GetEventsForHeightRange(
		"A.f8d6e0586b0a20c7.EVM.TransactionExecuted",
		startBlock.Height+1,
		endBlock.Height,
	)
	require.NoError(t, err)

	totalEVMEvents := 0
	now := blockEvents[0].BlockTimestamp.Add(-2 * time.Second)
	for _, blockEvent := range blockEvents {
		totalEVMEvents += len(blockEvent.Events)
		// Assert that each `EVM.TransactionExecuted` was included
		// in blocks were there was enough spacing. For Emulator,
		// blocks are created with each single Cadence transaction
		// execution.
		assert.GreaterOrEqual(t, blockEvent.BlockTimestamp.Sub(now), time.Second*2)
	}

	// Exactly 10 EVM transactions executed: no drops, no duplicates.
	assert.Equal(t, totalTxs, totalEVMEvents)
}

// Test_BatchTxPool_SingleTxImmediateSubmission asserts the fast path:
// a transaction with the expected next nonce, an empty queue and nothing
// in flight is submitted immediately as a single-tx batch, which takes
// the `EVM.run` path of the run.cdc script.
func Test_BatchTxPool_SingleTxImmediateSubmission(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	startBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	transferAmount := int64(50_000)

	signed, _, err := evmSign(
		big.NewInt(transferAmount),
		205_000,
		privateKey,
		0,
		&testEoaReceiver,
		nil,
	)
	require.NoError(t, err)

	_, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(transferAmount)) == 0
	}, time.Second*15, time.Second*1, "transaction was not executed")

	endBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	// Inspect the Cadence transactions submitted by the gateway between
	// the recorded heights. The transaction mempool always uses the run.cdc
	// script (recognizable by its `hexEncodedTxs` parameter), which uses
	// `EVM.run` for a single-tx array and `EVM.batchRun` for more.
	gatewayTxs := 0
	for height := startBlock.Height + 1; height <= endBlock.Height; height++ {
		block, err := emu.GetBlockByHeight(height)
		require.NoError(t, err)

		txResults, err := emu.GetTransactionsByBlockID(block.ID())
		require.NoError(t, err)

		for _, txResult := range txResults {
			script := string(txResult.Script)
			if !strings.Contains(script, "hexEncodedTxs") {
				continue
			}
			gatewayTxs++

			// Decode the `hexEncodedTxs` argument and assert that the
			// submission was a single-tx array, i.e. the `EVM.run` path.
			require.NotEmpty(t, txResult.Arguments)
			arg, err := jsonCdc.Decode(nil, txResult.Arguments[0])
			require.NoError(t, err)

			txsArray, ok := arg.(cadence.Array)
			require.True(t, ok)
			assert.Len(t, txsArray.Values, 1)
		}
	}

	// Exactly one gateway-submitted EVM transaction.
	assert.Equal(t, 1, gatewayTxs)
}

// Test_BatchTxPool_GapHoldAndFill asserts that transactions behind a
// nonce gap are held until the gap is filled, and that filling the gap
// releases the whole consecutive run.
func Test_BatchTxPool_GapHoldAndFill(t *testing.T) {
	_, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	transferAmount := int64(50_000)

	// Sign 5 transfers with nonces 0..4.
	signedTxs := make([][]byte, 5)
	for nonce := range 5 {
		signed, _, err := evmSign(
			big.NewInt(transferAmount),
			205_000,
			privateKey,
			uint64(nonce),
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)
		signedTxs[nonce] = signed
	}

	// Send nonces 0 and 1, wait until both are executed.
	for _, nonce := range []int{0, 1} {
		_, err := rpcTester.sendRawTx(signedTxs[nonce])
		require.NoError(t, err)
	}

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(2*transferAmount)) == 0
	}, time.Second*15, time.Second*1, "first two transactions were not executed")

	// Send nonces 3 and 4, skipping nonce 2: they must be held behind
	// the gap.
	for _, nonce := range []int{3, 4} {
		_, err := rpcTester.sendRawTx(signedTxs[nonce])
		require.NoError(t, err)
	}

	// Sleep past two flush ticks: the held transactions must NOT have
	// been submitted, because nonce 2 is missing.
	time.Sleep(2 * cfg.TxBatchInterval)

	balance, err := rpcTester.getBalance(testEoaReceiver)
	require.NoError(t, err)
	require.Zero(
		t,
		balance.Cmp(big.NewInt(2*transferAmount)),
		"transactions behind the nonce gap must be held, balance: %s", balance,
	)

	// Fill the gap with nonce 2: the whole consecutive run 2..4 is released.
	_, err = rpcTester.sendRawTx(signedTxs[2])
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(5*transferAmount)) == 0
	}, time.Second*30, time.Second*1, "all transactions were not executed")
}

// Test_BatchTxPool_BatchSizeCap asserts that the gateway never submits a
// Cadence transaction wrapping more EVM transactions than TxMaxBatchSize,
// while still executing every submitted transaction exactly once.
func Test_BatchTxPool_BatchSizeCap(t *testing.T) {
	const maxBatchSize = 5
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	totalTxs := 10
	transferAmount := int64(50_000)

	// Sign 10 transfers with consecutive nonces 0..9.
	signedTxs := make([][]byte, totalTxs)
	for nonce := range totalTxs {
		signed, _, err := evmSign(
			big.NewInt(transferAmount),
			205_000,
			privateKey,
			uint64(nonce),
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)
		signedTxs[nonce] = signed
	}

	startBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	// Send all 10 in nonce order. The fast path may submit the first alone;
	// the rest batch behind the collection window, capped at maxBatchSize.
	for _, signed := range signedTxs {
		_, err := rpcTester.sendRawTx(signed)
		require.NoError(t, err)
	}

	expectedBalance := int64(totalTxs) * transferAmount
	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(expectedBalance)) == 0
	}, time.Second*30, time.Second*1, "all transactions were not executed")

	endBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	// Inspect every gateway-submitted Cadence tx and count the EVM txs it
	// wraps (the `hexEncodedTxs` array length). Assertions are intentionally
	// robust rather than asserting an exact batch count: the fast path can
	// submit the first tx alone and timing affects how the rest group, so the
	// number of batches is not deterministic. What MUST hold is: every batch
	// honors the cap, the total across batches is exactly 10 (no drops/dupes),
	// and at least one batch carried more than one tx (proving batching via
	// EVM.batchRun actually happened rather than 10 single submissions).
	totalEVMTxs := 0
	maxObservedBatch := 0
	for height := startBlock.Height + 1; height <= endBlock.Height; height++ {
		block, err := emu.GetBlockByHeight(height)
		require.NoError(t, err)

		txResults, err := emu.GetTransactionsByBlockID(block.ID())
		require.NoError(t, err)

		for _, txResult := range txResults {
			if !strings.Contains(string(txResult.Script), "hexEncodedTxs") {
				continue
			}

			require.NotEmpty(t, txResult.Arguments)
			arg, err := jsonCdc.Decode(nil, txResult.Arguments[0])
			require.NoError(t, err)

			txsArray, ok := arg.(cadence.Array)
			require.True(t, ok)

			batchLen := len(txsArray.Values)
			assert.LessOrEqual(t, batchLen, maxBatchSize,
				"batch size cap of %d must be honored, got %d", maxBatchSize, batchLen)
			totalEVMTxs += batchLen
			if batchLen > maxObservedBatch {
				maxObservedBatch = batchLen
			}
		}
	}

	assert.Equal(t, totalTxs, totalEVMTxs, "every submitted EVM tx must appear exactly once")
	assert.Greater(t, maxObservedBatch, 1, "at least one batch must wrap more than one tx (EVM.batchRun)")
}

// Test_BatchTxPool_DuplicateTransactionRejection asserts that resending the
// exact same raw transaction while it is still QUEUED (held behind a nonce
// gap, not yet submitted) is rejected with ErrDuplicateTransaction.
func Test_BatchTxPool_DuplicateTransactionRejection(t *testing.T) {
	_, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	// Nonce 5 while the expected nonce is 0: the tx parks behind the gap and
	// stays queued (never fast-pathed, never in flight).
	signed, _, err := evmSign(
		big.NewInt(50_000),
		205_000,
		privateKey,
		5,
		&testEoaReceiver,
		nil,
	)
	require.NoError(t, err)

	// First send queues the transaction.
	_, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	// Resending the identical raw tx while it is still queued is rejected as
	// a duplicate (ErrDuplicateTransaction -> "transaction already in pool").
	_, err = rpcTester.sendRawTx(signed)
	require.Error(t, err)
	require.ErrorContains(t, err, errs.ErrDuplicateTransaction.Error())
}

// Test_BatchTxPool_InFlightNonceRejection asserts that a transaction
// carrying the same nonce as an in-flight submission is rejected, since
// it would burn Flow fees on a guaranteed nonce-mismatch failure.
func Test_BatchTxPool_InFlightNonceRejection(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	transferAmount := int64(50_000)

	// Sign two DIFFERENT transfers (different amounts, hence different
	// hashes) carrying the same nonce 0.
	signedFirst, _, err := evmSign(
		big.NewInt(transferAmount),
		205_000,
		privateKey,
		0,
		&testEoaReceiver,
		nil,
	)
	require.NoError(t, err)

	signedSecond, _, err := evmSign(
		big.NewInt(60_000),
		23_500,
		privateKey,
		0,
		&testEoaReceiver,
		nil,
	)
	require.NoError(t, err)

	// disable auto-mine so we can control delays
	emu.DisableAutoMine()

	// The first transfer takes the fast path: it is submitted immediately
	// and stays in flight until the local index confirms it.
	_, err = rpcTester.sendRawTx(signedFirst)
	require.NoError(t, err)

	// The second transfer is sent back to back with the first one: its
	// nonce is still in flight, so it must be rejected.
	_, err = rpcTester.sendRawTx(signedSecond)
	require.Error(t, err)
	require.ErrorContains(t, err, errs.ErrInFlightNonce.Error())

	// Execute and commit block, to advance EVM state
	_, _, err = emu.ExecuteAndCommitBlock()
	require.NoError(t, err)

	// The first transfer eventually executes.
	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(transferAmount)) == 0
	}, time.Second*15, time.Second*1, "first transaction was not executed")
}

func Test_TransactionBatchingMode(t *testing.T) {
	_, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")
	nonce := uint64(0)

	// test scenario for multiple same-EOA transactions with increasing nonce
	totalTxs := 25
	hashes := make([]common.Hash, totalTxs)
	for i := range totalTxs {
		signed, _, err := evmSign(big.NewInt(10), 205_000, eoaKey, nonce, &testAddr, nil)
		require.NoError(t, err)

		txHash, err := rpcTester.sendRawTx(signed)
		require.NoError(t, err)
		hashes[i] = txHash

		// Add a bit of random waiting time, to give the `BatchTxPool`
		// a chance to submit the pooled transactions in between requests.
		waitTime := rand.IntN(5) * 100
		time.Sleep(time.Duration(waitTime) * time.Millisecond)

		nonce += 1
	}

	assert.Eventually(t, func() bool {
		for _, h := range hashes {
			rcp, err := rpcTester.getReceipt(h.String())
			if err != nil || rcp == nil || rcp.Status != 1 {
				return false
			}
		}

		return true
	}, time.Second*25, time.Second*1, "all transactions were not executed")
}

func Test_TransactionBatchingModeWithConcurrentTxSubmissions(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddresses := map[common.Address]string{
		common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8"): "ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584",
		common.HexToAddress("0xF38079479cB8e3da977AF567c4B415c7f74f949E"): "ebac9d0795684b28d64402de1ad767ae875531929e15d105846781f8e3e2c214",
		common.HexToAddress("0xA675E5a2a26186cb5e70e7007e9c44F7fE6007F3"): "a39c2fcfc2a8f83d6cbbcd12b1c9184b7c03d71f3438b6b5a0b20a7f565c63ac",
		common.HexToAddress("0xc2a4d1f8A5A9308F65aDBb6f930Fb43BD73de533"): "21dc0a4f0ac11aded6ff24fd7f2c5d28af7bfee0daac26f3236956370d0cd751",
	}
	nonce := uint64(0)
	hashes := []common.Hash{}

	// Add a sufficient amount of funds to the test addresses
	for testAddr := range testAddresses {
		signed, _, err := evmSign(big.NewInt(1_000_000_000), 205_000, eoaKey, nonce, &testAddr, nil)
		require.NoError(t, err)

		txHash, err := rpcTester.sendRawTx(signed)
		require.NoError(t, err)
		hashes = append(hashes, txHash)

		nonce += 1
	}

	assert.Eventually(t, func() bool {
		for _, h := range hashes {
			rcp, err := rpcTester.getReceipt(h.String())
			if err != nil || rcp == nil || rcp.Status != 1 {
				return false
			}
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	totalTxs := 25
	transferAmount := int64(50_000)
	g := errgroup.Group{}

	startBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	for _, testPrivatekey := range testAddresses {
		privateKey, err := crypto.HexToECDSA(testPrivatekey)
		require.NoError(t, err)

		g.Go(func() error {
			nonce := uint64(0)

			for range totalTxs {
				signed, _, err := evmSign(
					big.NewInt(transferAmount),
					205_000,
					privateKey,
					nonce,
					&testEoaReceiver,
					nil,
				)
				if err != nil {
					return err
				}

				_, err = rpcTester.sendRawTx(signed)
				if err != nil {
					return err
				}

				waitTime := rand.IntN(5) * 100
				time.Sleep(time.Duration(waitTime) * time.Millisecond)

				nonce += 1
			}

			return nil
		})
	}

	err = g.Wait()
	require.NoError(t, err)

	expectedBalance := int64(len(testAddresses)) * int64(totalTxs) * transferAmount

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(expectedBalance)) == 0
	}, time.Second*30, time.Second*1, "all transactions were not executed")

	endBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	blockEvents, err := emu.GetEventsForHeightRange(
		"A.f8d6e0586b0a20c7.EVM.TransactionExecuted",
		startBlock.Height+1,
		endBlock.Height,
	)

	totalEVMEvents := 0
	for _, blockEvent := range blockEvents {
		totalEVMEvents += len(blockEvent.Events)
	}

	assert.Equal(t, 100, totalEVMEvents)
}

func Test_MultipleTransactionSubmissionsWithinSmallInterval(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privatekey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	// Add a sufficient amount of funds to the test address
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 205_000, eoaKey, 0, &testAddr, nil)
	require.NoError(t, err)

	txHash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		rcp, err := rpcTester.getReceipt(txHash.String())
		if err != nil || rcp == nil || rcp.Status != 1 {
			return false
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	latestBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	hashes := []common.Hash{}

	// We send 2 transactions without any delay between them.
	// Since there's no previous activity from the EOA,
	// the 1st transaction is submitted individually,
	// the 2nd transaction is added in the batch pool.
	for i := range uint64(2) {
		signed, _, err := evmSign(
			big.NewInt(500_000),
			205_000,
			privatekey,
			i,
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)

		txHash, err := rpcTester.sendRawTx(signed)
		require.NoError(t, err)

		hashes = append(hashes, txHash)
	}

	assert.Eventually(t, func() bool {
		for _, h := range hashes {
			rcp, err := rpcTester.getReceipt(h.String())
			if err != nil || rcp == nil || rcp.Status != 1 {
				return false
			}
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	block1, err := emu.GetBlockByHeight(latestBlock.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Assert that the 1st transaction was submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	block2, err := emu.GetBlockByHeight(latestBlock.Height + 2)
	require.NoError(t, err)

	txResults, err = emu.GetTransactionsByBlockID(block2.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Assert that the 2nd transaction was submitted in a batch.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.batchRun` instead of `EVM.run`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.batchRun",
	)
}

func Test_MultipleTransactionSubmissionsWithinRecentInterval(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privatekey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	// Add a sufficient amount of funds to the test address
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 205_000, eoaKey, 0, &testAddr, nil)
	require.NoError(t, err)

	txHash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		rcp, err := rpcTester.getReceipt(txHash.String())
		if err != nil || rcp == nil || rcp.Status != 1 {
			return false
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	latestBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	hashes := []common.Hash{}

	// We send 2 transactions with a 1 second delay between them.
	// Since there's no previous activity from the EOA,
	// the 1st transaction is submitted individually,
	// the 2nd transaction is added in the batch pool,
	// because the previous EOA activity is considered
	// recent (1 seconds ago).
	// A transaction is considered recent, when the last
	// activity of the EOA was X seconds ago, where:
	// X = `cfg.TxBatchInterval`.
	// For the E2E tests the `cfg.TxBatchInterval` is equal
	// to 2 seconds.
	for i := range uint64(2) {
		signed, _, err := evmSign(
			big.NewInt(500_000),
			205_000,
			privatekey,
			i,
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)

		// Add a 1 second delay before submitting the 2nd transaction
		if i == 1 {
			time.Sleep(time.Second)
		}

		txHash, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)

		hashes = append(hashes, txHash)
	}

	assert.Eventually(t, func() bool {
		for _, h := range hashes {
			rcp, err := rpcTester.getReceipt(h.String())
			if err != nil || rcp == nil || rcp.Status != 1 {
				return false
			}
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	block1, err := emu.GetBlockByHeight(latestBlock.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Assert that the 1st transaction was submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	block2, err := emu.GetBlockByHeight(latestBlock.Height + 2)
	require.NoError(t, err)

	txResults, err = emu.GetTransactionsByBlockID(block2.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Assert that the 2nd transaction was submitted in a batch.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.batchRun` instead of `EVM.run`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.batchRun",
	)
}

func Test_MultipleTransactionSubmissionsWithinNonRecentInterval(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privatekey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	// Add a sufficient amount of funds to the test address
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 205_000, eoaKey, 0, &testAddr, nil)
	require.NoError(t, err)

	txHash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		rcp, err := rpcTester.getReceipt(txHash.String())
		if err != nil || rcp == nil || rcp.Status != 1 {
			return false
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	latestBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	hashes := []common.Hash{}

	// We send 2 transactions with a delay of 3 seconds between them.
	// Since there's no previous activity from the EOA,
	// the 1st transaction is submitted individually,
	// the 2nd transaction is also submitted individually,
	// because the previous EOA activity is considered
	// non-recent (3 seconds ago).
	// A transaction is considered recent, when the last
	// activity of the EOA was X seconds ago, where:
	// X = `cfg.TxBatchInterval`.
	// For the E2E tests the `cfg.TxBatchInterval` is equal
	// to 2 seconds.
	for i := range uint64(2) {
		signed, _, err := evmSign(
			big.NewInt(500_000),
			205_000,
			privatekey,
			i,
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)

		// Add a 3 second delay before submitting the 2nd
		// transaction
		if i == 1 {
			time.Sleep(cfg.TxBatchInterval + time.Second)
		}

		txHash, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)

		hashes = append(hashes, txHash)
	}

	assert.Eventually(t, func() bool {
		for _, h := range hashes {
			rcp, err := rpcTester.getReceipt(h.String())
			if err != nil || rcp == nil || rcp.Status != 1 {
				return false
			}
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	block1, err := emu.GetBlockByHeight(latestBlock.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Assert that the 1st transaction was submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	block2, err := emu.GetBlockByHeight(latestBlock.Height + 2)
	require.NoError(t, err)

	txResults, err = emu.GetTransactionsByBlockID(block2.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Assert that the 2nd transaction was also submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)
}

func setupGatewayNode(t *testing.T) (emulator.Emulator, config.Config, func()) {
	srv, err := startEmulator(true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	emu := srv.Emulator()
	service := emu.ServiceKey()

	grpcHost := "localhost:3569"
	client, err := grpc.NewClient(grpcHost)
	require.NoError(t, err)

	// create new account with keys used for key-rotation
	coaAddress, privateKey, err := bootstrap.CreateMultiKeyAccount(
		client,
		200,
		service.Address,
		sc.FungibleToken.Address.HexWithPrefix(),
		sc.FlowToken.Address.HexWithPrefix(),
		service.PrivateKey,
	)
	require.NoError(t, err)

	cfg := config.Config{
		DatabaseDir:       t.TempDir(),
		AccessNodeHost:    grpcHost,
		RPCPort:           8545,
		RPCHost:           "127.0.0.1",
		FlowNetworkID:     "flow-emulator",
		EVMNetworkID:      types.FlowEVMPreviewNetChainID,
		Coinbase:          eoaTestAccount,
		COAAddress:        *coaAddress,
		COAKey:            privateKey,
		GasPrice:          new(big.Int).SetUint64(0),
		EnforceGasPrice:   true,
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         testLogWriter(),
		TxStateValidation: config.LocalIndexValidation,
		TxBatchMode:       true,
		TxBatchInterval:   time.Millisecond * 2500, // 2.5 seconds, the same as mainnet,
	}

	bootstrapDone := make(chan struct{})
	go func() {
		err = bootstrap.Run(ctx, cfg, func() {
			close(bootstrapDone)
		})
		require.NoError(t, err)
	}()

	<-bootstrapDone

	// Allow the Gateway to catch up on indexing
	time.Sleep(time.Second * 2)

	return emu, cfg, func() {
		cancel()
		srv.Stop()
	}
}
