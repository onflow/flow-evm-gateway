package integration

import (
	"context"
	"fmt"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/adapters"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-emulator/server"
	"github.com/onflow/flow-evm-gateway/services/events"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/memory"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"os"
	"time"
)

const testPrivateKey = "61ceacbdce419e25ee8e7c2beceee170a05c9cab1e725a955b15ba94dcd747d2"

var logger = zerolog.New(os.Stdout)

func startEmulator() (*server.EmulatorServer, error) {
	pkey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, testPrivateKey)
	if err != nil {
		return nil, err
	}

	log := logger.With().Str("component", "emulator").Logger().Level(zerolog.DebugLevel)
	srv := server.NewEmulatorServer(&log, &server.Config{
		ServicePrivateKey:  pkey,
		ServiceKeySigAlgo:  crypto.ECDSA_P256,
		ServiceKeyHashAlgo: crypto.SHA3_256,
		GenesisTokenSupply: 10000,
		EVMEnabled:         true,
		WithContracts:      true,
		Host:               "127.0.0.1",
	})

	go func() {
		defer srv.Stop()
		srv.Start()
	}()

	time.Sleep(200 * time.Millisecond) // give it some time to start, dummy but ok for test

	return srv, nil
}

// startEventIngestionEngine will start up the event engine with the grpc subscriber
// listening for events.
// todo for now we return index storage as a way to check the data if it was correctly
// indexed this will be in future replaced with evm gateway API access
func startEventIngestionEngine(ctx context.Context) (storage.BlockIndexer, storage.ReceiptIndexer, storage.TransactionIndexer, error) {
	client, err := grpc.NewClient("127.0.0.1:3569")
	if err != nil {
		return nil, nil, nil, err
	}

	blk, err := client.GetLatestBlock(ctx, false)
	if err != nil {
		return nil, nil, nil, err
	}

	subscriber := events.NewRPCSubscriber(client)
	blocks := memory.NewBlockStorage(memory.WithLatestHeight(blk.Height))
	receipts := memory.NewReceiptStorage()
	txs := memory.NewTransactionStorage()

	log := logger.With().Str("component", "ingestion").Logger()
	engine := events.NewEventIngestionEngine(subscriber, blocks, receipts, txs, log)

	go func() {
		err = engine.Start(ctx)
		if err != nil {
			logger.Error().Err(err)
			panic(err)
		}
	}()

	<-engine.Ready() // wait for engine to be ready
	return blocks, receipts, txs, err
}

// sendTransaction sends an evm transaction to the emulator, the code provided doesn't
// have to import EVM, this will be handled by this helper function.
func sendTransaction(emu emulator.Emulator, code string, args ...cadence.Value) error {
	key := emu.ServiceKey()

	codeWrapper := []byte(fmt.Sprintf(`
		import EVM from %s

		%s`,
		key.Address.HexWithPrefix(),
		code,
	))

	log := logger.With().Str("component", "adapter").Logger().Level(zerolog.InfoLevel)
	adapter := adapters.NewSDKAdapter(&log, emu)

	tx := sdk.NewTransaction().
		SetScript(codeWrapper).
		SetComputeLimit(flow.DefaultMaxTransactionGasLimit).
		SetProposalKey(key.Address, key.Index, key.SequenceNumber).
		SetPayer(key.Address)

	for _, arg := range args {
		err := tx.AddArgument(arg)
		if err != nil {
			return err
		}
	}

	signer, err := key.Signer()
	if err != nil {
		return err
	}

	err = tx.SignEnvelope(key.Address, key.Index, signer)
	if err != nil {
		return err
	}

	return adapter.SendTransaction(context.Background(), *tx)
}

func sendRawEVMTransaction() {

}
