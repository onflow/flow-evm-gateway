package version

import (
	"fmt"

	"github.com/onflow/flow-evm-gateway/api"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Prints the current version of the EVM Gateway Node",
	Run: func(*cobra.Command, []string) {
		fmt.Printf("%s\n", api.Version)
	},
}
