package tests

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/state"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

func Test_StateExecution(t *testing.T) {
	srv, err := startEmulator(true)
	require.NoError(t, err)

	emu := srv.Emulator()
	service := emu.ServiceKey()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
	}()

	cfg := &config.Config{
		InitCadenceHeight: 0,
		DatabaseDir:       t.TempDir(),
		FlowNetworkID:     flowGo.Emulator,
		HeartbeatInterval: 50,
		EVMNetworkID:      types.FlowEVMPreviewNetChainID,
		AccessNodeHost:    "localhost:3569",
		Coinbase:          common.HexToAddress(eoaTestAddress),
		COAAddress:        service.Address,
		COAKey:            service.PrivateKey,
		CreateCOAResource: true,
		GasPrice:          new(big.Int).SetUint64(0),
		LogLevel:          zerolog.DebugLevel,
		LogWriter:         zerolog.NewConsoleWriter(),
	}

	b, err := bootstrap.New(cfg)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	require.NoError(t, b.StartEventIngestion(ctx))
	require.NoError(t, b.StartAPIServer(ctx))
	require.NoError(t, b.StartStateIndex(ctx))

	blocks := b.Storages.Blocks
	receipts := b.Storages.Receipts
	store := b.Storages.Storage
	requester := b.Requester

	latest, err := blocks.LatestEVMHeight()
	require.NoError(t, err)

	block, err := blocks.GetByHeight(latest)
	require.NoError(t, err)

	st, err := state.NewState(block, pebble.NewLedger(store), cfg.FlowNetworkID, blocks, receipts)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")
	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)

	balance := st.GetBalance(testAddr)
	assert.Equal(t, uint64(0), balance.Uint64())

	amount := big.NewInt(1)
	evmTx, _, err := evmSign(amount, 21000, eoaKey, 0, &testAddr, nil)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	fmt.Println("-------------------------")
	hash, err := requester.SendRawTransaction(ctx, evmTx)
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	fmt.Println("-------------------------")
	time.Sleep(1 * time.Second) // wait for tx to get ingested

	latest, err = blocks.LatestEVMHeight()
	require.NoError(t, err)

	block, err = blocks.GetByHeight(latest)
	require.NoError(t, err)

	st, err = state.NewState(block, pebble.NewLedger(store), cfg.FlowNetworkID, blocks, receipts)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, amount.Uint64(), balance.Uint64())
}
