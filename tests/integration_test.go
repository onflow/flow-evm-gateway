package tests

import (
	"context"
	_ "embed"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"os"
	"testing"
	"time"
)

// Test_ConcurrentTransactionSubmission test submits multiple transactions concurrently
// and makes sure the transactions were submitted successfully. This is using the
// key-rotation signer that can handle multiple concurrent transactions.
func Test_ConcurrentTransactionSubmission(t *testing.T) {
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
	createdAddr, keys, err := bootstrap.CreateMultiKeyAccount(
		client,
		keyCount,
		service.Address,
		"0xee82856bf20e2aa6",
		"0x0ae53cb6e3f42a79",
		service.PrivateKey,
	)
	require.NoError(t, err)

	cfg := &config.Config{
		DatabaseDir:        t.TempDir(),
		AccessNodeGRPCHost: grpcHost,
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		FlowNetworkID:      "flow-emulator",
		EVMNetworkID:       types.FlowEVMTestnetChainID,
		Coinbase:           eoaTestAccount,
		COAAddress:         *createdAddr,
		COAKeys:            keys,
		CreateCOAResource:  true,
		GasPrice:           new(big.Int).SetUint64(0),
		LogLevel:           zerolog.DebugLevel,
		LogWriter:          os.Stdout,
	}

	// todo change this test to use ingestion and emulator directly so we can completely remove
	// the rpcTest implementation
	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()
	time.Sleep(500 * time.Millisecond) // some time to startup

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
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
