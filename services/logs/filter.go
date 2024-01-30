package logs

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

type Filter struct {
	addresses []common.Address
	topics    [][]common.Hash

	blockID    *common.Hash // Block hash if filtering a single block
	start, end *big.Int     // Range interval if filtering multiple blocks

	provider Provider
}

func (f *Filter) match() error {
	bloom := gethTypes.Bloom{} // create bloom

	logs, err := f.provider.Get(bloom, f.blockID, f.start, f.end)
	if err != nil {
		return err
	}

	for {
		select {
		case l := <-logs:

		}
	}
}
