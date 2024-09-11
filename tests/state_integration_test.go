package tests

import (
	"context"
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

func Test_StateExecution_Transfers(t *testing.T) {
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

	require.NoError(t, b.StartStateIndex(ctx))
	require.NoError(t, b.StartAPIServer(ctx))
	require.NoError(t, b.StartEventIngestion(ctx))

	blocks := b.Storages.Blocks
	receipts := b.Storages.Receipts
	store := b.Storages.Storage
	requester := b.EVMClient

	latest, err := blocks.LatestEVMHeight()
	require.NoError(t, err)

	block, err := blocks.GetByHeight(latest)
	require.NoError(t, err)

	// wait for emulator to boot
	time.Sleep(time.Second)

	register := pebble.NewRegister(store, latest)
	height0 := latest

	st, err := state.NewBlockState(block, register, cfg.FlowNetworkID, blocks, receipts, logger)
	require.NoError(t, err)

	testAddr := common.HexToAddress("55253ed90B70b96C73092D8680915aaF50081194")
	eoaKey, err := crypto.HexToECDSA(eoaTestPrivateKey)

	balance := st.GetBalance(testAddr)
	assert.Equal(t, uint64(0), balance.Uint64())

	amount := big.NewInt(1)
	nonce := uint64(0)
	evmTx, _, err := evmSign(amount, 21000, eoaKey, nonce, &testAddr, nil)
	require.NoError(t, err)

	hash, err := requester.SendRawTransaction(ctx, evmTx)
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	// wait for new block event
	time.Sleep(time.Second)
	latest, err = blocks.LatestEVMHeight()
	require.NoError(t, err)

	block, err = blocks.GetByHeight(latest)
	require.NoError(t, err)

	height1 := latest
	amount1 := amount.Uint64()

	register = pebble.NewRegister(store, latest)
	st, err = state.NewBlockState(block, register, cfg.FlowNetworkID, blocks, receipts, logger)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, amount.Uint64(), balance.Uint64())

	amount2 := big.NewInt(2)
	nonce++
	evmTx, _, err = evmSign(amount2, 21000, eoaKey, nonce, &testAddr, nil)
	require.NoError(t, err)

	hash, err = requester.SendRawTransaction(ctx, evmTx)
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	// wait for new block event, todo replace with better method
	time.Sleep(time.Second)
	latest, err = blocks.LatestEVMHeight()
	require.NoError(t, err)

	block, err = blocks.GetByHeight(latest)
	require.NoError(t, err)

	height2 := latest
	register = pebble.NewRegister(store, latest)
	st, err = state.NewBlockState(block, register, cfg.FlowNetworkID, blocks, receipts, logger)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, amount.Uint64()+amount2.Uint64(), balance.Uint64())

	// read balance at historic heights
	// height 0
	block, err = blocks.GetByHeight(height0)
	require.NoError(t, err)

	register = pebble.NewRegister(store, height0)
	st, err = state.NewBlockState(block, register, cfg.FlowNetworkID, blocks, receipts, logger)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, uint64(0), balance.Uint64())

	// height 1
	block, err = blocks.GetByHeight(height1)
	require.NoError(t, err)

	register = pebble.NewRegister(store, height1)
	st, err = state.NewBlockState(block, register, cfg.FlowNetworkID, blocks, receipts, logger)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, amount1, balance.Uint64())

	// height 2
	block, err = blocks.GetByHeight(height2)
	require.NoError(t, err)

	register = pebble.NewRegister(store, height2)
	st, err = state.NewBlockState(block, register, cfg.FlowNetworkID, blocks, receipts, logger)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, amount1+amount2.Uint64(), balance.Uint64())
}
