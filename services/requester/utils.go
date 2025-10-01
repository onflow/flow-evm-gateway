package requester

import (
	"fmt"
	"strings"

	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/flow-go/model/flow"
)

// ReplaceAddresses replace the addresses based on the network
func ReplaceAddresses(script []byte, chainID flow.ChainID) []byte {
	// make the list of all contracts we should replace address for
	sc := systemcontracts.SystemContractsForChain(chainID)
	contracts := []systemcontracts.SystemContract{
		sc.EVMContract,
		sc.FungibleToken,
		sc.FlowToken,
		sc.FlowFees,
	}

	s := string(script)
	// iterate over all the import name and address pairs and replace them in script
	for _, contract := range contracts {
		s = strings.ReplaceAll(s,
			fmt.Sprintf("import %s", contract.Name),
			fmt.Sprintf("import %s from %s", contract.Name, contract.Address.HexWithPrefix()),
		)
	}

	return []byte(s)
}
