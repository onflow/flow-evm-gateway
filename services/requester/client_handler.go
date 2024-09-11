package requester

import (
	"context"
	"math/big"

	"github.com/onflow/go-ethereum/common"
)

var _ EVMClient = &ClientHandler{}

// ClientHandler handles remote and local client for executing EVM operations.
// The handler contains logic that can switch between using local or remote client
// and implements error handling logic that can prefer either remote result or
// local result.
type ClientHandler struct {
	remote *RemoteClient
	local  *LocalClient
}

func (c *ClientHandler) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {

}

func (c *ClientHandler) GetBalance(ctx context.Context, address common.Address, evmHeight int64) (*big.Int, error) {

}

func (c *ClientHandler) Call(ctx context.Context, data []byte, from common.Address, evmHeight int64) ([]byte, error) {

}

func (c *ClientHandler) EstimateGas(ctx context.Context, data []byte, from common.Address, evmHeight int64) (uint64, error) {

}

func (c *ClientHandler) GetNonce(ctx context.Context, address common.Address, evmHeight int64) (uint64, error) {

}

func (c *ClientHandler) GetCode(ctx context.Context, address common.Address, evmHeight int64) ([]byte, error) {

}

func (c *ClientHandler) GetLatestEVMHeight(ctx context.Context) (uint64, error) {

}

func (c *ClientHandler) GetStorageAt(ctx context.Context, address common.Address, hash common.Hash, evmHeight int64) (common.Hash, error) {

}
