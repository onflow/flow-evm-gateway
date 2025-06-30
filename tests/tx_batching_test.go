package tests

import (
	"context"
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func Test_TransactionBatchingMode(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")
	nonce := uint64(0)

	// test scenario for multiple same-EOA transactions with increasing nonce
	totalTxs := 5*5 + 3
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
			if err != nil || rcp == nil || uint64(1) != rcp.Status {
				return false
			}
		}

		return true
	}, time.Second*15, time.Second*1, "all transactions were not executed")

	t.Run("same EOA transactions with mismatching nonces", func(t *testing.T) {
		// the first nonce will cause a "nonce too low" validation error
		// the second nonce will cause a "nonce too high" validation error
		nonces := []uint64{nonce - 2, nonce + 2}
		hashes := make([]common.Hash, len(nonces))

		for i, nonce := range nonces {
			signed, _, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
			require.NoError(t, err)

			txHash, err := rpcTester.sendRawTx(signed)
			require.NoError(t, err)
			hashes[i] = txHash
		}

		// give some time to BatchTxPool to submit the pooled transactions
		time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

		latestBlock, err := emu.GetLatestBlock()
		require.NoError(t, err)

		txResults, err := emu.GetTransactionResultsByBlockID(latestBlock.ID())
		require.NoError(t, err)
		require.Len(t, txResults, 1)

		txResult := txResults[0]
		// The `batch_run.cdc` Cadence transaction will fail with the error
		// message of the first invalid EVM transaction.
		expectedErrMessage := fmt.Sprintf(
			"nonce too low: address 0xFACF71692421039876a5BB4F10EF7A439D8ef61E, tx: %d state: %d",
			nonce-2,
			nonce,
		)
		assert.Contains(t, txResult.ErrorMessage, expectedErrMessage)
	})

	t.Run("same EOA transactions with mismatching nonces and valid nonce", func(t *testing.T) {
		// the first nonce will cause a "nonce too low" validation error
		// the second nonce will cause a "nonce too high" validation error
		// the third nonce, however, is valid and should execute
		nonces := []uint64{nonce - 2, nonce + 2, nonce}
		hashes := make([]common.Hash, len(nonces))

		for i, nonce := range nonces {
			signed, _, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
			require.NoError(t, err)

			txHash, err := rpcTester.sendRawTx(signed)
			require.NoError(t, err)
			hashes[i] = txHash
		}

		// assert that the EVM transaction that was signed with the
		// third nonce, was successfully executed.
		assert.Eventually(t, func() bool {
			rcp, err := rpcTester.getReceipt(hashes[2].String())
			if err != nil || rcp == nil || uint64(1) != rcp.Status {
				return false
			}

			return true
		}, time.Second*15, time.Second*1, "all transactions were not executed")

		latestBlock, err := emu.GetLatestBlock()
		require.NoError(t, err)

		txResults, err := emu.GetTransactionResultsByBlockID(latestBlock.ID())
		require.NoError(t, err)
		require.Len(t, txResults, 1)

		txResult := txResults[0]
		// assert that the Cadence transaction did not revert
		assert.Equal(t, "", txResult.ErrorMessage)

		nonce += 1
	})

	t.Run("same EOA transactions with valid nonce but non-sequential", func(t *testing.T) {
		// the three nonces below are all valid, but they are
		// non-sequential. Due to the threaded nature of the EVM GW,
		// we sort the grouped transactions of EOAs by their nonce.
		nonces := []uint64{nonce + 2, nonce, nonce + 1}
		hashes := make([]common.Hash, len(nonces))

		for i, nonce := range nonces {
			signed, _, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
			require.NoError(t, err)

			txHash, err := rpcTester.sendRawTx(signed)
			require.NoError(t, err)
			hashes[i] = txHash
		}

		assert.Eventually(t, func() bool {
			for _, h := range hashes {
				rcp, err := rpcTester.getReceipt(h.String())
				if err != nil || rcp == nil || uint64(1) != rcp.Status {
					return false
				}
			}

			return true
		}, time.Second*15, time.Second*1, "all transactions were not executed")

		latestBlock, err := emu.GetLatestBlock()
		require.NoError(t, err)

		txResults, err := emu.GetTransactionResultsByBlockID(latestBlock.ID())
		require.NoError(t, err)
		require.Len(t, txResults, 1)

		txResult := txResults[0]
		assert.Equal(t, "", txResult.ErrorMessage)

		nonce += 3
	})

	stop()
}

func Test_TransactionBatchingModeWithConcurrentTxSubmissions(t *testing.T) {
	_, cfg, stop := setupGatewayNode(t)

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

	// Add a sufficient amount of funds to the test addresses
	for testAddr := range testAddresses {
		signed, _, err := evmSign(big.NewInt(1_000_000_000), 23_500, eoaKey, nonce, &testAddr, nil)
		require.NoError(t, err)

		_, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)

		nonce += 1
	}

	// some time to process the transfer transactions
	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	totalTxs := 25
	transferAmount := int64(50_000)
	g := errgroup.Group{}

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

	stop()
}

func Test_MultipleTransactionSubmissionsWithinSmallInterval(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)

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

	_, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	latestBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	// We send 2 transactions without any delay between them.
	// Since there's no previous activity from the EOA,
	// the 1st transaction is submitted individually,
	// the 2nd transaction is added in the batch pool.
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

		_, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)
	}

	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	block1, err := emu.GetBlockByHeight(latestBlock.Header.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.Len(t, txResults, 1)

	// Assert that the 1st transaction was submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	block2, err := emu.GetBlockByHeight(latestBlock.Header.Height + 2)
	require.NoError(t, err)

	txResults, err = emu.GetTransactionsByBlockID(block2.ID())
	require.NoError(t, err)
	require.Len(t, txResults, 1)

	// Assert that the 2nd transaction was submitted in a batch.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.batchRun` instead of `EVM.run`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.batchRun",
	)

	stop()
}

func Test_MultipleTransactionSubmissionsWithinRecentInterval(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)

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

	_, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	latestBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	// We send 2 transactions with a 3 second delay between them.
	// Since there's no previous activity from the EOA,
	// the 1st transaction is submitted individually,
	// the 2nd transaction is added in the batch pool,
	// because the previous EOA activity is considered
	// recent (3 seconds ago).
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
			time.Sleep(time.Second * 3)
		}

		_, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)
	}

	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	block1, err := emu.GetBlockByHeight(latestBlock.Header.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.Len(t, txResults, 1)

	// Assert that the 1st transaction was submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	block2, err := emu.GetBlockByHeight(latestBlock.Header.Height + 2)
	require.NoError(t, err)

	txResults, err = emu.GetTransactionsByBlockID(block2.ID())
	require.NoError(t, err)
	require.Len(t, txResults, 1)

	// Assert that the 2nd transaction was submitted in a batch.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.batchRun` instead of `EVM.run`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.batchRun",
	)

	stop()
}

func Test_MultipleTransactionSubmissionsWithinNonRecentInterval(t *testing.T) {
	emu, cfg, stop := setupGatewayNode(t)

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

	_, err = rpcTester.sendRawTx(signed)
	require.NoError(t, err)

	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	latestBlock, err := emu.GetLatestBlock()
	require.NoError(t, err)

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	// We send 2 transactions with a 5 second delay between them.
	// Since there's no previous activity from the EOA,
	// the 1st transaction is submitted individually,
	// the 2nd transaction is added in the batch pool,
	// because the previous EOA activity is considered
	// non-recent (5 seconds ago).
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

		// Add a 5 second delay before submitting the 2nd
		// transaction
		if i == 1 {
			time.Sleep(time.Second * 5)
		}

		_, err = rpcTester.sendRawTx(signed)
		require.NoError(t, err)
	}

	time.Sleep(cfg.TxBatchInterval + (1 * time.Second))

	block1, err := emu.GetBlockByHeight(latestBlock.Header.Height + 1)
	require.NoError(t, err)

	txResults, err := emu.GetTransactionsByBlockID(block1.ID())
	require.NoError(t, err)
	require.Len(t, txResults, 1)

	// Assert that the 1st transaction was submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	block2, err := emu.GetBlockByHeight(latestBlock.Header.Height + 2)
	require.NoError(t, err)

	txResults, err = emu.GetTransactionsByBlockID(block2.ID())
	require.NoError(t, err)
	require.Len(t, txResults, 1)

	// Assert that the 2nd transaction was also submitted individually.
	// The easiest way to check that is by making sure that the
	// Cadence tx used `EVM.run` instead of `EVM.batchRun`.
	assert.Contains(
		t,
		string(txResults[0].Script),
		"EVM.run",
	)

	stop()
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
	keyCount := 5
	coaAddress, privateKey, err := bootstrap.CreateMultiKeyAccount(
		client,
		keyCount,
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
		TxBatchInterval:   time.Millisecond * 1200,
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
