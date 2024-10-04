package api

import (
	"fmt"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/go-ethereum/rpc"
)

func resolveBlockNumberOrHash(
	block *rpc.BlockNumberOrHash,
	blocksDB storage.BlockIndexer,
) (uint64, error) {
	err := fmt.Errorf("%w: neither block number nor hash specified", errs.ErrInvalid)
	if block == nil {
		return 0, err
	}
	if number, ok := block.Number(); ok {
		return resolveBlockNumber(number, blocksDB)
	}

	if hash, ok := block.Hash(); ok {
		evmHeight, err := blocksDB.GetHeightByID(hash)
		if err != nil {
			return 0, err
		}
		return evmHeight, nil
	}

	return 0, err
}

func resolveBlockNumber(
	number rpc.BlockNumber,
	blocksDB storage.BlockIndexer,
) (uint64, error) {
	height := number.Int64()

	// if special values (latest) we return latest executed height
	if height < 0 {
		executed, err := blocksDB.LatestEVMHeight()
		if err != nil {
			return 0, err
		}
		height = int64(executed)
	}

	return uint64(height), nil
}
