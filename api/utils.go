package api

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/metrics"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
)

func resolveBlockTag(
	blockNumberOrHash *rpc.BlockNumberOrHash,
	blocksDB storage.BlockIndexer,
	logger zerolog.Logger,
) (uint64, error) {
	if blockNumberOrHash == nil {
		return 0, fmt.Errorf(
			"%w: neither block number nor hash specified",
			errs.ErrInvalid,
		)
	}
	if number, ok := blockNumberOrHash.Number(); ok {
		height, err := resolveBlockNumber(number, blocksDB)
		if err != nil {
			logger.Error().Err(err).
				Stringer("block_number", number).
				Msg("failed to resolve block by number")
			return 0, err
		}
		return height, nil
	}

	if hash, ok := blockNumberOrHash.Hash(); ok {
		height, err := blocksDB.GetHeightByID(hash)
		if err != nil {
			logger.Error().Err(err).
				Stringer("block_hash", hash).
				Msg("failed to resolve block by hash")
			return 0, err
		}
		return height, nil
	}

	return 0, fmt.Errorf(
		"%w: neither block number nor hash specified",
		errs.ErrInvalid,
	)
}

func resolveBlockNumber(
	blockNumber rpc.BlockNumber,
	blocksDB storage.BlockIndexer,
) (uint64, error) {
	// if special values (latest) we return latest executed height
	//
	// all the special values are:
	//	EarliestBlockNumber  = BlockNumber(-5)
	//	SafeBlockNumber      = BlockNumber(-4)
	//	FinalizedBlockNumber = BlockNumber(-3)
	//	LatestBlockNumber    = BlockNumber(-2)
	//	PendingBlockNumber   = BlockNumber(-1)
	switch blockNumber {
	case rpc.EarliestBlockNumber:
		// the earliest block is the genesis block, which has a block number of `0`
		return 0, nil
	case rpc.SafeBlockNumber,
		rpc.FinalizedBlockNumber,
		rpc.LatestBlockNumber,
		rpc.PendingBlockNumber:
		// EVM on Flow does not have these concepts,
		// but the latest block is the closest fit
		height, err := blocksDB.LatestEVMHeight()
		if err != nil {
			return 0, err
		}
		return height, nil
	}

	return uint64(blockNumber), nil
}

// decodeHash parses a hex-encoded 32-byte hash. The input may optionally
// be prefixed by 0x and can have a byte length up to 32.
func decodeHash(s string) (h common.Hash, err error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	if (len(s) & 1) > 0 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid hex string: %s", s)
	}
	if len(b) > common.HashLength {
		return common.Hash{}, fmt.Errorf(
			"hex string too long, want at most 32 bytes, have %d bytes",
			len(b),
		)
	}
	return common.BytesToHash(b), nil
}

// handleError takes in an error and in case the error is of type ErrEntityNotFound
// it returns nil instead of an error since that is according to the API spec,
// if the error is not of type ErrEntityNotFound it will return the error and the generic
// empty type.
func handleError[T any](err error, log zerolog.Logger, collector metrics.Collector) (T, error) {
	var (
		zero        T
		revertedErr *errs.RevertError
	)

	switch {
	// as per specification returning nil and nil for not found resources
	case errors.Is(err, errs.ErrEntityNotFound):
		return zero, nil
	case errors.Is(err, errs.ErrInvalid):
		return zero, err
	case errors.Is(err, errs.ErrFailedTransaction):
		return zero, err
	case errors.As(err, &revertedErr):
		return zero, revertedErr
	case errors.Is(err, core.ErrNonceTooLow):
		return zero, err
	case errors.Is(err, core.ErrNonceTooHigh):
		return zero, err
	case errors.Is(err, core.ErrInsufficientFunds):
		return zero, err
	case errors.Is(err, errs.ErrRateLimit):
		return zero, err
	default:
		collector.ApiErrorOccurred()
		log.Error().Err(err).Msg("api error")
		return zero, err
	}
}
