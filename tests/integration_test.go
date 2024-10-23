package tests

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onflow/crypto/hash"
	"github.com/onflow/flow-go-sdk/access/grpc"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/onflow/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
)

const startingBlockHeight = uint64(3)

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
		DatabaseDir:       t.TempDir(),
		AccessNodeHost:    grpcHost,
		RPCPort:           8545,
		RPCHost:           "127.0.0.1",
		FlowNetworkID:     "flow-emulator",
		EVMNetworkID:      types.FlowEVMPreviewNetChainID,
		Coinbase:          eoaTestAccount,
		COAAddress:        *createdAddr,
		COAKeys:           keys,
		CreateCOAResource: true,
		GasPrice:          new(big.Int).SetUint64(0),
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         testLogWriter(),
	}

	// todo change this test to use ingestion and emulator directly so we can completely remove
	// the rpcTest implementation
	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	ready := make(chan struct{})
	go func() {
		err := bootstrap.Run(ctx, cfg, ready)
		require.NoError(t, err)
	}()

	<-ready

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")

	// disable auto-mine so we can control delays
	emu.DisableAutoMine()

	totalTxs := keyCount*5 + 3
	hashes := make([]common.Hash, totalTxs)
	nonce := uint64(0)
	for i := 0; i < totalTxs; i++ {
		signed, signedHash, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
		require.NoError(t, err)
		hashes[i] = signedHash

		// send raw transaction waits for transaction result and blocks, but since we don't have the result
		// available until block is committed bellow we must continue without waiting, we will get all the
		// transaction receipts later to confirm transactions have been successful, we only add a bit of delay
		// to ensure order of transactions was correct, because there is no other way to proceed once transaction
		// is submitted over network
		go rpcTester.sendRawTx(signed)
		time.Sleep(50 * time.Millisecond)

		// execute commit block every 3 blocks so we make sure we should have conflicts with seq numbers if keys not rotated
		if i%3 == 0 {
			_, _, _ = emu.ExecuteAndCommitBlock()
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
	}, time.Second*30, time.Second*1, "all transactions were not executed")
}

func Test_EthClientTest(t *testing.T) {
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
		DatabaseDir:       t.TempDir(),
		AccessNodeHost:    grpcHost,
		RPCPort:           8545,
		RPCHost:           "127.0.0.1",
		FlowNetworkID:     "flow-emulator",
		EVMNetworkID:      types.FlowEVMPreviewNetChainID,
		Coinbase:          eoaTestAccount,
		COAAddress:        *createdAddr,
		COAKeys:           keys,
		CreateCOAResource: true,
		GasPrice:          new(big.Int).SetUint64(150),
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         testLogWriter(),
	}

	ready := make(chan struct{})
	go func() {
		err := bootstrap.Run(ctx, cfg, ready)
		require.NoError(t, err)
	}()

	<-ready

	ethClient, err := ethclient.Dial("http://127.0.0.1:8545")
	require.NoError(t, err)

	blockNumber, err := ethClient.BlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(startingBlockHeight+2), blockNumber)

	block, err := ethClient.BlockByNumber(context.Background(), big.NewInt(int64(blockNumber)))
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(int64(startingBlockHeight)+2), block.Number())
}

func Test_CloudKMSConcurrentTransactionSubmission(t *testing.T) {
	// When this env var is missing, we simply skip the entire test
	// as it requires access to Google Cloud KMS to properly run.
	// Run from command line, in the `tests` directory, with:
	// GOOGLE_APPLICATION_CREDENTIALS="/some-path/google-app-creds-c97e30ff5f30.json" go test -timeout 130s -run ^Test_CloudKMSConcurrentTransactionSubmission$ github.com/onflow/flow-evm-gateway/integration
	// Make sure to update the slice of `publicKeys` below.
	googleAppCreds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if len(googleAppCreds) == 0 {
		t.Skip()
	}

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

	kmsKeyIDs := []string{
		"gw-key-6", "gw-key-7", "gw-key-8", "gw-key-9", "gw-key-10",
	}
	kmsKeys := make([]flowGoKMS.Key, len(kmsKeyIDs))
	for i, keyID := range kmsKeyIDs {
		kmsKeys[i] = flowGoKMS.Key{
			ProjectID:  "flow-evm-gateway",
			LocationID: "global",
			KeyRingID:  "tx-signing",
			KeyID:      keyID,
			KeyVersion: "1",
		}
	}

	kmsClient, err := flowGoKMS.NewClient(ctx)
	require.NoError(t, err)
	publicKeys := make([]string, len(kmsKeyIDs))
	for i, kmsKey := range kmsKeys {
		publicKey, hashAlgo, err := kmsClient.GetPublicKey(ctx, kmsKey)
		require.NoError(t, err)
		require.Equal(t, hash.SHA2_256, hashAlgo)
		publicKeys[i] = strings.Replace(publicKey.String(), "0x", "", 1)
	}

	keyCount := len(kmsKeyIDs)
	// create new account with Cloud KMS keys used for key-rotation
	createdAddr, err := bootstrap.CreateMultiCloudKMSKeysAccount(
		client,
		publicKeys,
		service.Address,
		"0xee82856bf20e2aa6",
		"0x0ae53cb6e3f42a79",
		service.PrivateKey,
	)
	require.NoError(t, err)

	cfg := &config.Config{
		DatabaseDir:       t.TempDir(),
		AccessNodeHost:    grpcHost,
		RPCPort:           8545,
		RPCHost:           "127.0.0.1",
		FlowNetworkID:     "flow-emulator",
		EVMNetworkID:      types.FlowEVMPreviewNetChainID,
		Coinbase:          eoaTestAccount,
		COAAddress:        *createdAddr,
		COACloudKMSKeys:   kmsKeys,
		CreateCOAResource: true,
		GasPrice:          new(big.Int).SetUint64(0),
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         testLogWriter(),
	}

	// todo change this test to use ingestion and emulator directly so we can completely remove
	// the rpcTest implementation
	rpcTester := &rpcTest{
		url: fmt.Sprintf("%s:%d", cfg.RPCHost, cfg.RPCPort),
	}

	ready := make(chan struct{})
	go func() {
		err := bootstrap.Run(ctx, cfg, ready)
		require.NoError(t, err)
	}()

	<-ready

	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")

	// disable auto-mine so we can control delays
	emu.DisableAutoMine()

	totalTxs := keyCount*5 + 3
	hashes := make([]common.Hash, totalTxs)
	nonce := uint64(0)
	for i := 0; i < totalTxs; i++ {
		signed, signedHash, err := evmSign(big.NewInt(10), 21000, eoaKey, nonce, &testAddr, nil)
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
		nonce += 1
	}

	time.Sleep(5 * time.Second) // wait for all txs to be executed

	for _, h := range hashes {
		rcp, err := rpcTester.getReceipt(h.String())
		require.NoError(t, err)
		assert.Equal(t, uint64(1), rcp.Status)
	}
}
