package export

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/offchain/storage"
	flowGo "github.com/onflow/flow-go/model/flow"
)

var Cmd = &cobra.Command{
	Use:   "export-evm-state",
	Short: "Export EVM state at a specific height",
	RunE: func(*cobra.Command, []string) error {
		if height == 0 || outputDir == "" || registerStoreDir == "" {
			return fmt.Errorf("all flags (height, output, register-store) must be provided")
		}

		log.Info().Msgf("exporting EVM state for height %v from registerStoreDir %v, outputDir: %v, chain: %v", height, registerStoreDir, outputDir, chain)

		chainID := flowGo.ChainID(chain)

		err := ExportEVMStateForHeight(height, outputDir, registerStoreDir, chainID)
		if err != nil {
			return fmt.Errorf("fail to export: %w", err)
		}

		log.Info().Msgf("successfully exported EVM state to %v", outputDir)

		return nil
	},
}

var (
	height           uint64
	outputDir        string
	chain            string
	registerStoreDir string
)

func init() {
	Cmd.Flags().Uint64Var(&height, "evm-height", 0, "EVM Block height for EVM state export")
	Cmd.Flags().StringVar(&outputDir, "output", "", "Output directory for exported EVM state")
	Cmd.Flags().StringVar(&chain, "chain-id", "testnet", "Chain ID for the EVM state")
	Cmd.Flags().StringVar(&registerStoreDir, "register-store", "", "Directory of the register store")
}

func ExportEVMStateForHeight(height uint64, outputDir string, registerStoreDir string, chainID flowGo.ChainID) error {
	storageAddress := evm.StorageAccountAddress(chainID)

	pebbleDB, err := pebble.OpenDB(registerStoreDir)
	if err != nil {
		return fmt.Errorf("failed to open pebble db: %w", err)
	}

	store := pebble.New(pebbleDB, log.Logger)
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
