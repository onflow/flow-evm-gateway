package tests

import (
	"context"
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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
		signed, _, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
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
	}, time.Second*15, time.Second*1, "all transactions were not executed")
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
		signed, _, err := evmSign(big.NewInt(1_000_000_000), 23_500, eoaKey, nonce, &testAddr, nil)
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
					23_500,
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
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 23_500, eoaKey, 0, &testAddr, nil)
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
	// Both are now always enqueued in the pool regardless of prior EOA
	// activity, so they accumulate and are submitted together on the next
	// flush tick as a single EVM.batchRun Cadence transaction.
	for i := range uint64(2) {
		signed, _, err := evmSign(
			big.NewInt(500_000),
			23_500,
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

	// Both transactions arrive before the first flush tick, so they are
	// batched together into one Cadence transaction in the first EVM block
	// after latestBlock.
	block1, err := emu.GetBlockByHeight(latestBlock.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

	// Both transactions must be submitted together in a single EVM.batchRun
	// Cadence transaction — not split across two blocks.
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
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 23_500, eoaKey, 0, &testAddr, nil)
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

	// We send 2 transactions with a 1-second delay between them (less than
	// TxBatchInterval of 2 s). Both arrive before the flush tick fires, so
	// they accumulate in the pool and are submitted together as EVM.batchRun.
	for i := range uint64(2) {
		signed, _, err := evmSign(
			big.NewInt(500_000),
			23_500,
			privatekey,
			i,
			&testEoaReceiver,
			nil,
		)
		require.NoError(t, err)

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

	// Both transactions land in the same Cadence transaction (EVM.batchRun)
	// because they were both in the pool when the flush tick fired.
	block1, err := emu.GetBlockByHeight(latestBlock.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.True(t, len(txResults) >= 1)

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
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 23_500, eoaKey, 0, &testAddr, nil)
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

	// We send 2 transactions with a 3-second delay between them, which is
	// greater than TxBatchInterval (2 s). The first transaction is pooled and
	// flushed alone before the second arrives, so each transaction is
	// submitted in its own Cadence transaction via EVM.run (single-element
	// batch).
	for i := range uint64(2) {
		signed, _, err := evmSign(
			big.NewInt(500_000),
			23_500,
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

func Test_MultipleTransactionSubmissionsWithDuplicates(t *testing.T) {
	_, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")
	nonce := uint64(0)
	hashes := make([]common.Hash, 0)

	signed, _, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
	require.NoError(t, err)

	txHash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)
	hashes = append(hashes, txHash)

	// Increment nonce for the duplicate test transactions that follow
	nonce += 1
	dupSigned, _, err := evmSign(big.NewInt(10), 15_000_000, eoaKey, nonce, &testAddr, nil)
	require.NoError(t, err)

	// Submit 5 identical transactions to test duplicate detection:
	// the first should succeed, the rest should be rejected as duplicates.
	for i := range 5 {
		if i == 0 {
			txHash, err := rpcTester.sendRawTx(dupSigned)
			require.NoError(t, err)
			hashes = append(hashes, txHash)
		} else {
			_, err := rpcTester.sendRawTx(dupSigned)
			require.Error(t, err)
			require.ErrorContains(t, err, "invalid: transaction already in pool")
		}
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
}

// Test_OutOfOrderNonceBurstTransactions is a regression test for the
// "first tx escapes" bug that caused silent transaction drops for wallets
// sending parallel bursts.
//
// # Background
//
// The original BatchTxPool submitted the first transaction from any burst
// immediately (before the rest of the burst arrived), bypassing the pool.
// If that first transaction happened to carry a future nonce — which is
// common when multiple goroutines race to submit — it failed on-chain because
// the preceding nonces hadn't been processed yet. EVM.batchRun then stopped
// at the resulting nonce gap and dropped all transactions after it.
//
// # Confirmed on testnet (2026-06-03)
//
// Wallet: 0x75346056F36342c699e8aDf4e4D2692c20aDcc9e (Flow EVM testnet, chain 545)
// Test:   10 transactions sent in parallel, shuffled nonce order [8 7 3 9 2 1 6 0 4 5]
//
//   eth_sendRawTransaction: 10/10 returned success   ← gateway accepted all
//   landed on-chain:         6/10                    ← 4 silently dropped
//
// Nonce 6 arrived first at the gateway, had no prior EOA activity, and was
// submitted immediately. It failed (on-chain nonce was 0). The batch received
// the remaining 9 nonces sorted as [0 1 2 3 4 5 7 8 9]. EVM.batchRun
// executed nonces 0–5 successfully, then hit nonce 7 (gap at 6) and stopped.
//
// # Fix
//
// BatchTxPool.Add now always enqueues every transaction — including the first
// from a burst — into the pool. The flush timer is the sole submission path,
// guaranteeing that the full burst accumulates before any of it is sent.
func Test_OutOfOrderNonceBurstTransactions(t *testing.T) {
	_, cfg, stop := setupGatewayNode(t)
	defer stop()

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("0x061B63D29332e4de81bD9F51A48609824CD113a8")
	privatekey, err := crypto.HexToECDSA("ddcb1e965557474fd13de3a66a40e4bc9b759a306e5db1046bac5ca47aafd584")
	require.NoError(t, err)

	// Fund the test wallet
	signed, _, err := evmSign(big.NewInt(1_000_000_000), 23_500, eoaKey, 0, &testAddr, nil)
	require.NoError(t, err)

	fundHash, err := rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		rcp, err := rpcTester.getReceipt(fundHash.String())
		return err == nil && rcp != nil && rcp.Status == 1
	}, time.Second*15, time.Second*1, "funding transaction was not executed")

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")
	const burstSize = 10

	// Build all transactions upfront with sequential nonces 0..burstSize-1.
	txs := make([][]byte, burstSize)
	hashes := make([]string, burstSize)
	for i := range burstSize {
		s, hash, err := evmSign(big.NewInt(1_000), 23_500, privatekey, uint64(i), &testEoaReceiver, nil)
		require.NoError(t, err)
		txs[i] = s
		hashes[i] = hash.Hex()
	}

	// Shuffle the submission order to simulate out-of-sequence parallel dispatch
	// (the same pattern used by DFNS and similar high-volume wallet providers).
	shuffled := rand.Perm(burstSize)
	g := errgroup.Group{}
	for _, idx := range shuffled {
		i := idx
		g.Go(func() error {
			_, err := rpcTester.sendRawTx(txs[i])
			return err
		})
	}
	require.NoError(t, g.Wait(), "all eth_sendRawTransaction calls must succeed")

	// All burstSize transactions must land on-chain. Prior to the fix, 4 of
	// 10 were silently dropped when the first-to-arrive nonce was non-zero.
	assert.Eventually(t, func() bool {
		for _, h := range hashes {
			rcp, err := rpcTester.getReceipt(h)
			if err != nil || rcp == nil || rcp.Status != 1 {
				return false
			}
		}
		return true
	}, time.Second*30, time.Second*1,
		"all %d burst transactions must land on-chain; some were likely dropped due to nonce gaps", burstSize)
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
		TxStateValidation: config.TxSealValidation,
		TxBatchMode:       true,
		TxBatchInterval:   time.Second * 2,
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
