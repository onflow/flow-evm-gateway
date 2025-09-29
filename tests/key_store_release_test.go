package tests

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
)

func Test_KeyStoreSigningKeysRelease(t *testing.T) {
	srv, err := startEmulator(true, defaultServerConfig())
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

	// Poll for emulator readiness
	require.Eventually(t, func() bool {
		err := client.Ping(ctx)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "emulator failed to start")

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
		DatabaseDir:        t.TempDir(),
		AccessNodeHost:     grpcHost,
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		FlowNetworkID:      "flow-emulator",
		EVMNetworkID:       types.FlowEVMPreviewNetChainID,
		Coinbase:           eoaTestAccount,
		COAAddress:         *createdAddr,
		COAKey:             privateKey,
		COATxLookupEnabled: true,
		GasPrice:           new(big.Int).SetUint64(0),
		EnforceGasPrice:    true,
		LogLevel:           zerolog.DebugLevel,
		LogWriter:          testLogWriter(),
		TxStateValidation:  config.LocalIndexValidation,
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

	time.Sleep(3 * time.Second) // some time for EVM GW to startup

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")

	// The first 6 nonces: [5, 4, 3, 2, 1, 1], are all invalid EVM
	// transactions that will fail with `nonce too high`, because
	// fresh account starts at nonce 0.
	// Given that we created our COA above with a `keyCount = 5`,
	// we assert that transaction submission still functions properly
	// and that signing keys get released frequently, with each new
	// Flow block.
	// The rest 8 nonces: [0, 1, 5, 2, 5, 3, 5, 4], contain 5 valid
	// EVM transactions (nonces 0, 1, 2, 3, 4) and 3 invalid ones
	// (the duplicate nonce 5 submissions).
	// Finally, we assert that `testAddr` has the expected balance
	// from the 5 valid EVM transactions, which are transfers of
	// equal amounts.
	nonces := []uint64{5, 4, 3, 2, 1, 1, 0, 1, 5, 2, 5, 3, 5, 4}
	transferAmount := int64(50_000)
	for _, nonce := range nonces {
		signed, _, err := evmSign(big.NewInt(transferAmount), 23_000, eoaKey, nonce, &testAddr, nil)
		require.NoError(t, err)

		txHash, err := rpcTester.sendRawTx(signed)
		// For nonces > 4 initially or out of sequence,
		// we expect "nonce too high" errors but we still
		// submit them to test key release.
		if err != nil {
			t.Logf("transaction with nonce %d returned error: %v", nonce, err)
		} else {
			t.Logf("transaction with nonce %d submitted: %s", nonce, txHash)
		}
	}

	expectedBalance := int64(keyCount) * transferAmount
	assert.Eventually(t, func() bool {
		balance, err := rpcTester.getBalance(testAddr)
		require.NoError(t, err)

		return balance.Cmp(big.NewInt(expectedBalance)) == 0
	}, time.Second*15, time.Second*1, "all transactions were not executed")
}
