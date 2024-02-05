package integration

import (
	"github.com/onflow/flow-emulator/server"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/rs/zerolog"
	"os"
)

func StartEmulator() {
	logger := zerolog.New(os.Stdout)

	pkey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, "123")
	if err != nil {
		panic(err)
	}

	srv := server.NewEmulatorServer(&logger, &server.Config{
		// BlockTime:       0, maybe
		ServicePrivateKey:  pkey,
		ServiceKeySigAlgo:  crypto.ECDSA_P256,
		ServiceKeyHashAlgo: crypto.SHA3_256,
		GenesisTokenSupply: 10000,
		EVMEnabled:         true,
		WithContracts:      true,
		Host:               "localhost",
	})

	go func() {
		srv.Start()
	}()

}
