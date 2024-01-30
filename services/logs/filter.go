package logs

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"slices"
)

type Filter struct {
	address common.Address
	topics  []common.Hash

	// todo maybe move to get method
	blockID    *common.Hash // Block hash if filtering a single block
	start, end *big.Int     // Range interval if filtering multiple blocks

	provider Provider
}

func (f *Filter) get() error {
	bloom := gethTypes.Bloom{} // create bloom

	logs, err := f.provider.Get(bloom, f.blockID, f.start, f.end)
	if err != nil {
		return err
	}

	for {
		select {
		case l := <-logs:
			if f.matches(l) {
				// todo
			}
		}
	}
}

func (f *Filter) matches(log *gethTypes.Log) bool {
	if len(f.topics) > len(log.Topics) {
		return false
	}

	for _, topic := range f.topics {
		if !slices.Contains(log.Topics, topic) {
			return false
		}
	}

	if f.address.Cmp(log.Address) != 0 {
		return false
	}

	return true
}
