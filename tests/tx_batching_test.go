package tests

import (
	"context"
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func Test_TransactionBatchingMode(t *testing.T) {
	srv, err := startEmulator(true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		srv.Stop()
	}()

	grpcHost := "localhost:3569"
	emu := srv.Emulator()
	service := emu.ServiceKey()

	client, err := grpc.NewClient(grpcHost)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond) // some time to startup

	// create new account with keys used for key-rotation
	keyCount := 5
	createdAddr, privateKey, err := bootstrap.CreateMultiKeyAccount(
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
		COAAddress:        *createdAddr,
		COAKey:            privateKey,
		GasPrice:          new(big.Int).SetUint64(0),
		EnforceGasPrice:   true,
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         testLogWriter(),
		TxStateValidation: config.TxSealValidation,
		TxBatchMode:       true,
		TxBatchInterval:   time.Millisecond * 1200,
	}

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	ready := make(chan struct{})
	go func() {
		err = bootstrap.Run(ctx, cfg, func() {
			close(ready)
		})
		require.NoError(t, err)
	}()

	<-ready

	time.Sleep(3 * time.Second) // some time to startup

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")
	nonce := uint64(0)

	// test scenario for multiple same-EOA transactions with increasing nonce
	totalTxs := keyCount*5 + 3
	hashes := make([]common.Hash, totalTxs)
	for i := range totalTxs {
		signed, _, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
		require.NoError(t, err)

		txHash, err := rpcTester.sendRawTx(signed)
		require.NoError(t, err)
		hashes[i] = txHash

		// Add a bit of waiting time every 3 to 5 transactions, to give the
		// `BatchTxPool` a chance to submit the pooled transactions.
		// The waiting time is about the same as the mainnet block production
		// rate.
		if i%3 == 0 || i%5 == 0 {
			time.Sleep(time.Millisecond * 800)
		}
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

		var latestBlock *flow.Block
		require.Eventually(t, func() bool {
			latestBlock, err = emu.GetLatestBlock()
			require.NoError(t, err)

			return latestBlock != nil
		}, time.Second*15, time.Second*1, "latest block could not be fetched")

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

		var latestBlock *flow.Block
		require.Eventually(t, func() bool {
			latestBlock, err = emu.GetLatestBlock()
			require.NoError(t, err)

			return latestBlock != nil
		}, time.Second*15, time.Second*1, "latest block could not be fetched")

		txResults, err := emu.GetTransactionResultsByBlockID(latestBlock.ID())
		require.NoError(t, err)
		require.Len(t, txResults, 1)

		txResult := txResults[0]
		// assert that the Cadence transaction did not revert
		assert.Equal(t, "", txResult.ErrorMessage)

		// assert that the EVM transaction that was signed with the
		// third nonce, was successfully executed.
		rcp, err := rpcTester.getReceipt(hashes[2].String())
		require.NoError(t, err)
		assert.Equal(t, uint64(1), rcp.Status)

		nonce += 1
	})

	t.Run("same EOA transactions with valid nonce but non-sequential", func(t *testing.T) {
		// the three nonces below are all valid, but they are
		// non-sequential. Due to the thread nature of the EVM GW,
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

		var latestBlock *flow.Block
		require.Eventually(t, func() bool {
			latestBlock, err = emu.GetLatestBlock()
			require.NoError(t, err)

			return latestBlock != nil
		}, time.Second*15, time.Second*1, "latest block could not be fetched")

		txResults, err := emu.GetTransactionResultsByBlockID(latestBlock.ID())
		require.NoError(t, err)
		require.Len(t, txResults, 1)

		txResult := txResults[0]
		assert.Equal(t, "", txResult.ErrorMessage)

		assert.Eventually(t, func() bool {
			for _, h := range hashes {
				rcp, err := rpcTester.getReceipt(h.String())
				if err != nil || rcp == nil || uint64(1) != rcp.Status {
					return false
				}
			}

			return true
		}, time.Second*15, time.Second*1, "all transactions were not executed")

		nonce += 3
	})
}

func Test_TransactionBatchingModeWithConcurrentTxSubmissions(t *testing.T) {
	srv, err := startEmulator(true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		srv.Stop()
	}()

	grpcHost := "localhost:3569"
	emu := srv.Emulator()
	service := emu.ServiceKey()

	client, err := grpc.NewClient(grpcHost)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond) // some time to startup

	// create new account with keys used for key-rotation
	keyCount := 5
	createdAddr, privateKey, err := bootstrap.CreateMultiKeyAccount(
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
		COAAddress:        *createdAddr,
		COAKey:            privateKey,
		GasPrice:          new(big.Int).SetUint64(0),
		EnforceGasPrice:   true,
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         testLogWriter(),
		TxStateValidation: config.TxSealValidation,
		TxBatchMode:       true,
		TxBatchInterval:   time.Millisecond * 1200,
	}

	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	ready := make(chan struct{})
	go func() {
		err = bootstrap.Run(ctx, cfg, func() {
			close(ready)
		})
		require.NoError(t, err)
	}()

	<-ready

	time.Sleep(3 * time.Second) // some time to startup

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

	time.Sleep(3 * time.Second) // some time to process the transfer transactions

	testEoaReceiver := common.HexToAddress("0x6F416dcC9BEFe43b7dDF53f2662F76dD34A9fc11")

	totalTxs := 25
	g := errgroup.Group{}
	var err1 error

	for _, testPrivatekey := range testAddresses {
		privateKey, err := crypto.HexToECDSA(testPrivatekey)
		require.NoError(t, err)

		g.Go(func() error {
			nonce := uint64(0)

			for range totalTxs {
				signed, _, err := evmSign(big.NewInt(50_000), 23_500, privateKey, nonce, &testEoaReceiver, nil)
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
	require.NoError(t, err1)

	expectedBalance := int64(4 * totalTxs * 50_000)

	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testEoaReceiver)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(expectedBalance)) == 0
	}, time.Second*10, time.Second*1, "all transactions were not executed")
}
