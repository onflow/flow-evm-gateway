package requester

import (
	"context"
	"math/big"

	"github.com/onflow/go-ethereum/common"
)

type EVMClient interface {
	// SendRawTransaction will submit signed transaction data to the network.
	// The submitted EVM transaction hash is returned.
	SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error)

	// GetBalance returns the amount of wei for the given address in the state of the
	// given EVM block height.
	GetBalance(ctx context.Context, address common.Address, evmHeight int64) (*big.Int, error)

	// Call executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(ctx context.Context, data []byte, from common.Address, evmHeight int64) ([]byte, error)

	// EstimateGas executes the given signed transaction data on the state for the given EVM block height.
	// Note, this function doesn't make any changes in the state/blockchain and is
	// useful to executed and retrieve the gas consumption and possible failures.
	EstimateGas(ctx context.Context, data []byte, from common.Address, evmHeight int64) (uint64, error)

	// GetNonce gets nonce from the network at the given EVM block height.
	GetNonce(ctx context.Context, address common.Address, evmHeight int64) (uint64, error)

	// GetCode returns the code stored at the given address in
	// the state for the given EVM block height.
	GetCode(ctx context.Context, address common.Address, evmHeight int64) ([]byte, error)

	// GetLatestEVMHeight returns the latest EVM height of the network.
	GetLatestEVMHeight(ctx context.Context) (uint64, error)

	// GetStorageAt returns the storage from the state at the given address, key and block number.
	GetStorageAt(ctx context.Context, address common.Address, hash common.Hash, evmHeight int64) (common.Hash, error)
}
