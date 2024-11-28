package main

import (
	"flag"
	"os"

	"github.com/rs/zerolog/log"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/offchain/storage"
	flowGo "github.com/onflow/flow-go/model/flow"
)

func main() {
	var (
		height           uint64
		outputDir        string
		registerStoreDir string
	)

	flag.Uint64Var(&height, "evm-height", 0, "EVM Block height for EVM state export")
	flag.StringVar(&outputDir, "output", "", "Output directory for exported EVM state")
	flag.StringVar(&registerStoreDir, "register-store", "", "Directory of the register store")

	flag.Parse()

	if height == 0 || outputDir == "" || registerStoreDir == "" {
		log.Error().Msg("All flags (height, output, register-store) must be provided")
		flag.Usage()
		os.Exit(1)
	}

	chainID := flowGo.Testnet

	log.Info().Msgf("exporting EVM state for height %v from registerStoreDir %v, outputDir: %v", height, registerStoreDir, outputDir)
	err := ExportEVMStateForHeight(height, outputDir, registerStoreDir, chainID)
	if err != nil {
		log.Fatal().Err(err).Msgf("fail to export")
	}

	log.Info().Msgf("successfully exported EVM state to %v", outputDir)
}

func ExportEVMStateForHeight(height uint64, outputDir string, registerStoreDir string, chainID flowGo.ChainID) error {
	store, err := pebble.New(registerStoreDir, log.Logger)
	if err != nil {
		return err
	}

	storageAddress := evm.StorageAccountAddress(chainID)
	registerStore := pebble.NewRegisterStorage(store, storageAddress)
	snapshot, err := registerStore.GetSnapshotAt(height)
	if err != nil {
		return err
	}

	ledger := storage.NewReadOnlyStorage(snapshot)
	exporter, err := state.NewExporter(ledger, storageAddress)
	if err != nil {
		return err
	}

	err = exporter.ExportGob(outputDir)
	if err != nil {
		return err
	}

	return nil
}
