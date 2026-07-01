package tests

import (
	"context"
	"fmt"
	"math/big"
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
)

// Test_TxMemPool_OutOfOrderBurst is the DFNS regression scenario:
// a burst of transactions from a single EOA, arriving out of nonce order,
// must all be executed exactly once - no drops, no duplicates.
func Test_TxMemPool_OutOfOrderBurst(t *testing.T) {
	emu, cfg, stop := setupTxMemPoolGatewayNode(t, 10)
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

// Test_TxMemPool_SingleTxImmediateSubmission asserts the fast path:
// a transaction with the expected next nonce, an empty queue and nothing
// in flight is submitted immediately as a single-tx batch, which takes
// the `EVM.run` path of the run.cdc script.
func Test_TxMemPool_SingleTxImmediateSubmission(t *testing.T) {
	emu, cfg, stop := setupTxMemPoolGatewayNode(t, 10)
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

// Test_TxMemPool_GapHoldAndFill asserts that transactions behind a
// nonce gap are held until the gap is filled, and that filling the gap
// releases the whole consecutive run.
func Test_TxMemPool_GapHoldAndFill(t *testing.T) {
	_, cfg, stop := setupTxMemPoolGatewayNode(t, 10)
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

	// Sleep past the collection window (300ms) and the submission spacing
	// (1200ms), but well within the pool TTL (5s): the held transactions
	// must NOT have been submitted.
	time.Sleep(2500 * time.Millisecond)

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

// Test_TxMemPool_TTLExpiry asserts that a transaction held behind a
// nonce gap that never fills is evicted from the pool once `TxPoolTTL`
// expires (it is submitted on-chain anyway, where it fails, instead of
// being silently dropped).
func Test_TxMemPool_TTLExpiry(t *testing.T) {
	_, cfg, stop := setupTxMemPoolGatewayNode(t, 10)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privateKey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	fundEOA(t, rpcTester, testAddr)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	// Sign a transfer with nonce 5, while the expected nonce is 0:
	// it will be held behind the gap.
	signed, _, err := evmSign(
		big.NewInt(50_000),
		205_000,
		privateKey,
		5,
		&testEoaReceiver,
		nil,
	)
	require.NoError(t, err)

	// The first send queues the transaction without errors.
	_, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	// An exact duplicate of a queued transaction is rejected.
	_, err = rpcTester.sendRawTx(signed)
	require.Error(t, err)
	require.ErrorContains(t, err, "transaction already in pool")

	// The gap never fills, so the transaction cannot execute.
	time.Sleep(2 * time.Second)

	balance, err := rpcTester.getBalance(testEoaReceiver)
	require.NoError(t, err)
	require.Zero(t, balance.Sign(), "held transaction must not have executed")

	// Once the pool TTL (5s) expires, the held transaction is evicted
	// from the queue (and submitted on-chain, where it fails with a nonce
	// mismatch). The eviction is proven by the duplicate check no longer
	// firing: resending the identical raw transaction succeeds again.
	assert.Eventually(t, func() bool {
		_, err := rpcTester.sendRawTx(signed)
		return err == nil
	}, time.Second*15, time.Millisecond*500, "held transaction was not evicted after TTL")

	// Nonce 5 can never execute while the expected nonce is 0.
	balance, err = rpcTester.getBalance(testEoaReceiver)
	require.NoError(t, err)
	require.Zero(t, balance.Sign(), "transaction with nonce gap must never execute")
}

// Test_TxMemPool_InFlightNonceRejection asserts that a transaction
// carrying the same nonce as an in-flight submission is rejected, since
// it would burn Flow fees on a guaranteed nonce-mismatch failure.
func Test_TxMemPool_InFlightNonceRejection(t *testing.T) {
	_, cfg, stop := setupTxMemPoolGatewayNode(t, 10)
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

	// The first transfer takes the fast path: it is submitted immediately
	// and stays in flight until the local index confirms it.
	_, err = rpcTester.sendRawTx(signedFirst)
	require.NoError(t, err)

	// The second transfer is sent back to back with the first one: its
	// nonce is still in flight, so it must be rejected. This relies on the
	// emulator block production + gateway ingestion taking well over the
	// sub-millisecond gap between the two sends.
	_, err = rpcTester.sendRawTx(signedSecond)
	require.Error(t, err)
	require.ErrorContains(t, err, "transaction with the same nonce already submitted")

	// The first transfer eventually executes.
	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(transferAmount)) == 0
	}, time.Second*15, time.Second*1, "first transaction was not executed")
}

// Test_TxMemPool_BatchSizeCap asserts that the gateway never submits a
// Cadence transaction wrapping more EVM transactions than TxMaxBatchSize,
// while still executing every submitted transaction exactly once.
func Test_TxMemPool_BatchSizeCap(t *testing.T) {
	const maxBatchSize = 5
	emu, cfg, stop := setupTxMemPoolGatewayNode(t, maxBatchSize)
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

// Test_TxMemPool_DuplicateTransactionRejection asserts that resending the
// exact same raw transaction while it is still QUEUED (held behind a nonce
// gap, not yet submitted) is rejected with ErrDuplicateTransaction.
//
// A fast-path-submitted tx instead becomes IN-FLIGHT, so resending it would
// surface as ErrInFlightNonce (see Test_TxMemPool_InFlightNonceRejection).
// To reach the duplicate path we must keep the tx queued: send an
// out-of-order nonce so it parks behind a gap, then resend the identical
// bytes before the gap fills.
func Test_TxMemPool_DuplicateTransactionRejection(t *testing.T) {
	_, cfg, stop := setupTxMemPoolGatewayNode(t, 10)
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
	require.ErrorContains(t, err, "transaction already in pool")
}

// fundEOA adds a sufficient amount of funds to the given test EOA, from
// the test EOA created on emulator setup, and waits until the funding
// transaction is executed.
func fundEOA(t *testing.T, rpcTester *rpcTest, testAddr common.Address) {
	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	signed, _, err := evmSign(big.NewInt(1_000_000_000), 205_000, eoaKey, 0, &testAddr, nil)
	require.NoError(t, err)

	txHash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		rcp, err := rpcTester.getReceipt(txHash.String())
		return err == nil && rcp != nil && rcp.Status == 1
	}, time.Second*15, time.Second*1, "funding transaction was not executed")
}

// setupTxMemPoolGatewayNode starts an emulator and a gateway node
// configured with the transaction mempool.
//
// The tests use TxSealValidation like the existing batching tests, so
// API-level state validation doesn't race the local index; the pool itself
// reads the local index for expected nonces, which the ingestion engine
// populates in the emulator.
// TxPoolTTL is 5s so the TTL test runs quickly; maxBatchSize is supplied by
// the caller (10 lets a 10-tx burst fit in one batch; smaller values exercise
// the batch-size cap).
func setupTxMemPoolGatewayNode(t *testing.T, maxBatchSize int) (emulator.Emulator, config.Config, func()) {
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
		DatabaseDir:         t.TempDir(),
		AccessNodeHost:      grpcHost,
		RPCPort:             8545,
		RPCHost:             "127.0.0.1",
		FlowNetworkID:       "flow-emulator",
		EVMNetworkID:        types.FlowEVMPreviewNetChainID,
		Coinbase:            eoaTestAccount,
		COAAddress:          *coaAddress,
		COAKey:              privateKey,
		GasPrice:            new(big.Int).SetUint64(0),
		EnforceGasPrice:     true,
		LogLevel:            zerolog.DebugLevel,
		LogWriter:           testLogWriter(),
		TxStateValidation:   config.TxSealValidation,
		TxMemPoolMode:       true,
		TxCollectionWindow:  300 * time.Millisecond,
		TxSubmissionSpacing: 1200 * time.Millisecond,
		TxPoolTTL:           5 * time.Second,
		TxMaxBatchSize:      maxBatchSize,
	}

	bootstrapDone := make(chan struct{})
	go func() {
		err = bootstrap.Run(ctx, cfg, func() {
			close(bootstrapDone)
		})
		require.NoError(t, err)
	}()

	<-bootstrapDone

	return emu, cfg, func() {
		cancel()
		srv.Stop()
	}
}
