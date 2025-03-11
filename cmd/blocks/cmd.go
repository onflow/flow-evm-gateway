package blocks

import (
	"errors"
	"fmt"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	flowGo "github.com/onflow/flow-go/model/flow"
)

var Cmd = &cobra.Command{
	Use:   "check-blocks-integrity",
	Short: "Checks the EVM blocks integrity. Note that the given DB dir should not be currently in use by a running node.",
	RunE: func(*cobra.Command, []string) error {
		pebbleDB, err := pebble.OpenDB(databaseDir)
		if err != nil {
			return fmt.Errorf("failed to open pebble db: %w", err)
		}
		defer pebbleDB.Close()
		store := pebble.New(pebbleDB, log.Logger)

		blocks := pebble.NewBlocks(store, flowGo.ChainID(chainID))

		latestHeight, err := blocks.LatestEVMHeight()
		if err != nil {
			return fmt.Errorf("failed to get latest EVM height: %w", err)
		}
		log.Info().Msgf("Checking for missing EVM blocks up to EVM height %d", latestHeight)

		var missingBlocks []uint64
		for height := uint64(0); height <= latestHeight; height++ {
			_, err := blocks.GetByHeight(height)
			if errors.Is(err, errs.ErrEntityNotFound) {
				log.Error().Msgf("missing EVM block with height: %d", height)
				missingBlocks = append(missingBlocks, height)
			} else if err != nil {
				log.Error().Err(err).Msgf("failed to get block at height: %d", height)
			}
		}

		if len(missingBlocks) > 0 {
			log.Error().Msgf("Found %d missing blocks in the EVM blocks database", len(missingBlocks))
			return nil
		}

		log.Info().Msg("EVM blocks DB has no integrity issues. All blocks are indexed.")

		return nil
	},
}

var (
	databaseDir string
	chainID     string
)

func init() {
	Cmd.Flags().StringVar(&databaseDir, "database-dir", "./db", "Path to the directory for the database")
	Cmd.Flags().StringVar(&chainID, "chain-id", "testnet", "Chain ID for the EVM network")
	Cmd.MarkFlagRequired("database-dir")
	Cmd.MarkFlagRequired("chain-id")
}
