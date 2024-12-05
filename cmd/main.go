package main

import (
	"os"

	"github.com/onflow/flow-evm-gateway/cmd/export-evm-state"
	"github.com/onflow/flow-evm-gateway/cmd/run"
	"github.com/onflow/flow-evm-gateway/cmd/version"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "",
	Short: "Utility commands for the EVM Gateway Node",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Err(err).Msg("failed to run command")
		os.Exit(1)
	}
}

func main() {
	rootCmd.AddCommand(version.Cmd)
	rootCmd.AddCommand(export.Cmd)
	rootCmd.AddCommand(run.Cmd)

	Execute()
}
