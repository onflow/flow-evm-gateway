package requester

import (
	"fmt"
	"strings"

	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/flow-go/model/flow"
)

// replaceAddresses replace the addresses based on the network
func replaceAddresses(script []byte, chainID flow.ChainID) []byte {
	// make the list of all contracts we should replace address for
	sc := systemcontracts.SystemContractsForChain(chainID)
	contracts := []systemcontracts.SystemContract{
		sc.EVMContract,
		sc.FungibleToken,
		sc.FlowToken,
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
