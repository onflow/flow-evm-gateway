package integration

import (
	"context"
	"fmt"
	"github.com/onflow/flow-evm-gateway/storage/memory"
	"github.com/onflow/flow-go-sdk/access/grpc"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/adapters"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-emulator/server"
	"github.com/onflow/flow-evm-gateway/services/events"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"os"
)

const testPrivateKey = "61ceacbdce419e25ee8e7c2beceee170a05c9cab1e725a955b15ba94dcd747d2"

var logger = zerolog.New(os.Stdout)

func startEmulator() (*server.EmulatorServer, error) {
	pkey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, testPrivateKey)
	if err != nil {
		return nil, err
	}

	srv := server.NewEmulatorServer(&logger, &server.Config{
		ServicePrivateKey:  pkey,
		ServiceKeySigAlgo:  crypto.ECDSA_P256,
		ServiceKeyHashAlgo: crypto.SHA3_256,
		GenesisTokenSupply: 10000,
		EVMEnabled:         true,
		WithContracts:      true,
		Host:               grpc.EmulatorHost,
	})

	return srv, srv.Listen()
}

func startEventIngestionEngine(ctx context.Context) error {
	client, err := grpc.NewClient(grpc.EmulatorHost, nil)
	if err != nil {
		return err
	}

	subscriber := events.NewRPCSubscriber(client)
	blocks := memory.NewBlockStorage()
	receipts := memory.NewReceiptStorage()
	txs := memory.NewTransactionStorage()

	engine := events.NewEventIngestionEngine(subscriber, blocks, receipts, txs, logger)

	go func() {
		err = engine.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	<-engine.Ready() // wait for engine to be ready
	return nil
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

	adapter := adapters.NewSDKAdapter(&logger, emu)

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
