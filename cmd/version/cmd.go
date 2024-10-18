package version

import (
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Prints the current version of the EVM Gateway Node",
	Run: func(*cobra.Command, []string) {
		log.Info().Str("version", api.Version).Msg("build details")
	},
}
