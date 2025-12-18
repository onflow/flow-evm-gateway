package replayer

import (
	"github.com/onflow/flow-go/model/flow"
)

type Config struct {
	ChainID         flow.ChainID
	RootAddr        flow.Address
	ValidateResults bool
}
