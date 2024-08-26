package tests

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/onflow/cadence"
	"github.com/onflow/cadence/encoding/json"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/requester"
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

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	store, err := pebble.New(cfg.DatabaseDir, logger)
	require.NoError(t, err)

	blocks := pebble.NewBlocks(store, cfg.FlowNetworkID)
	transactions := pebble.NewTransactions(store)
	receipts := pebble.NewReceipts(store)
	accounts := pebble.NewAccounts(store)

	blocksPublisher := models.NewPublisher()
	logsPublisher := models.NewPublisher()

	currentSporkClient, err := grpc.NewClient(cfg.AccessNodeHost)
	require.NoError(t, err)

	client, err := requester.NewCrossSporkClient(
		currentSporkClient,
		nil,
		logger,
		cfg.FlowNetworkID,
	)
	require.NoError(t, err)

	cadenceBlock, err := client.GetBlockHeaderByHeight(ctx, cfg.InitCadenceHeight)
	require.NoError(t, err)

	err = blocks.InitHeights(cfg.InitCadenceHeight, cadenceBlock.ID)
	require.NoError(t, err)

	subscriber := ingestion.NewRPCSubscriber(
		client,
		cfg.HeartbeatInterval,
		cfg.FlowNetworkID,
		logger,
	)

	eventEngine := ingestion.NewEventIngestionEngine(
		subscriber,
		store,
		blocks,
		receipts,
		transactions,
		accounts,
		blocksPublisher,
		logsPublisher,
		logger,
		metrics.NopCollector,
	)

	go func() {
		err = eventEngine.Run(ctx)
		if err != nil {
			logger.Error().Err(err).Msg("event ingestion engine failed to run")
			panic(err)
		}
	}()

	// wait for ingestion engines to be ready
	<-eventEngine.Ready()

	stateEngine := state.NewStateEngine(
		cfg,
		pebble.NewLedger(store),
		blocksPublisher,
		blocks,
		transactions,
		receipts,
		logger,
	)
	if err := stateEngine.Run(ctx); err != nil {
		logger.Error().Err(err).Msg("state engine failed to run")
		panic(err)
	}

	// wait for state engine to be ready
	<-stateEngine.Ready()

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

	// todo reuse
	script := []byte(`import EVM from 0xf8d6e0586b0a20c7

transaction(hexEncodedTx: String) {
    let coa: &EVM.CadenceOwnedAccount

    prepare(signer: auth(Storage) &Account) {
        self.coa = signer.storage.borrow<&EVM.CadenceOwnedAccount>(
            from: /storage/evm
        ) ?? panic("Could not borrow reference to the COA!")
    }

    execute {
        let txResult = EVM.run(
            tx: hexEncodedTx.decodeHex(),
            coinbase: self.coa.address()
        )
        assert(
            txResult.status == EVM.Status.successful,
            message: "evm_error=".concat(txResult.errorMessage).concat("\n")
        )
    }
}
`)

	txData := hex.EncodeToString(evmTx)
	hexEncodedTx, err := cadence.NewString(txData)
	require.NoError(t, err)
	arg, err := json.Encode(hexEncodedTx)
	require.NoError(t, err)

	latestBlock, err := client.GetLatestBlock(ctx, true)
	require.NoError(t, err)

	tx := flowGo.NewTransactionBody().
		SetReferenceBlockID(flowGo.Identifier(latestBlock.ID)).
		SetScript(script).
		SetPayer(flowGo.Address(service.Address)).
		SetProposalKey(flowGo.Address(service.Address), 0, 0).
		AddArgument(arg).
		AddAuthorizer(flowGo.Address(service.Address))

	err = emu.SendTransaction(tx)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	latest, err = blocks.LatestEVMHeight()
	require.NoError(t, err)

	block, err = blocks.GetByHeight(latest)
	require.NoError(t, err)

	st, err = state.NewState(block, pebble.NewLedger(store), cfg.FlowNetworkID, blocks, receipts)
	require.NoError(t, err)

	balance = st.GetBalance(testAddr)
	assert.Equal(t, amount.Uint64(), balance.Uint64())
}
