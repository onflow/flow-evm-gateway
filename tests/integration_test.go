package tests

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/onflow/flow-go-sdk/access/grpc"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
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
		DatabaseDir:       t.TempDir(),
		AccessNodeHost:    grpcHost,
		RPCPort:           8545,
		RPCHost:           "127.0.0.1",
		FlowNetworkID:     "flow-emulator",
		EVMNetworkID:      types.FlowEVMTestNetChainID,
		Coinbase:          eoaTestAccount,
		COAAddress:        *createdAddr,
		COAKeys:           keys,
		CreateCOAResource: true,
		GasPrice:          new(big.Int).SetUint64(0),
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         os.Stdout,
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

func Test_CloudKMSConcurrentTransactionSubmission(t *testing.T) {
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

	// create new account with Cloud KMS keys used for key-rotation
	publicKeys := []string{
		"3549d9d17014d02feb159c5069fd79c2290b075cce8496c476dad6aadad4cefb91928c1ab2709652fc46cfbafb8bac89844a305da3382c4aebb13e9525698daa",
		"9d5d95dd245b48c37bebb15d92d6e20b069ee1118acb313f1adf43a05e1fc37fdd1f266f756610ddd7fb32ca4285d0c46358170bdc2ff96ce9dd1796e5a302ba",
		"1208f683ece6b3b3d275edf7a8356a5b5d21cddab5e013329ed025148ce48f338e9461444fe555f61a09eeac739072cdf8f23bb58675308197a3f82b3ad41c3c",
		"13f6be6ead79eea18c86a22e9eb7dcedf2729641c1e8823acbab9fd9ed88668056a0d102b5455f486fc630ca028b8e171794dfab5291c88f079bc2a8fd23f28f",
		"b83ff59a869799cc7df80cdd7e69c9df35df93658beeb201a942b74e8f8417cb4c28555235986e53e61fd83b762e1337a720f266649ea3f24801b3d8f1341487",
	}
	keyCount := len(publicKeys)
	createdAddr, err := bootstrap.CreateMultiCloudKMSKeysAccount(
		client,
		publicKeys,
		service.Address,
		"0xee82856bf20e2aa6",
		"0x0ae53cb6e3f42a79",
		service.PrivateKey,
	)
	require.NoError(t, err)
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

	cfg := &config.Config{
		DatabaseDir:       t.TempDir(),
		AccessNodeHost:    grpcHost,
		RPCPort:           8545,
		RPCHost:           "127.0.0.1",
		FlowNetworkID:     "flow-emulator",
		EVMNetworkID:      types.FlowEVMTestNetChainID,
		Coinbase:          eoaTestAccount,
		COAAddress:        *createdAddr,
		COACloudKMSKeys:   kmsKeys,
		CreateCOAResource: true,
		GasPrice:          new(big.Int).SetUint64(0),
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         os.Stdout,
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
	time.Sleep(2500 * time.Millisecond) // some time to startup

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
