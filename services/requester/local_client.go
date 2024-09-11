package requester

import (
	"context"
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/services/state"
	"github.com/onflow/flow-evm-gateway/storage"
)

var _ EVMClient = &LocalClient{}

type LocalClient struct {
	state  *state.BlockState
	blocks storage.BlockIndexer
}

func (l *LocalClient) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	return common.Hash{}, fmt.Errorf("local client is read-only")
}

func (l *LocalClient) GetBalance(ctx context.Context, address common.Address, evmHeight int64) (*big.Int, error) {
	bal := l.state.GetBalance(address)
	return (&big.Int{}).SetUint64(bal.Uint64()), nil
}

func (l *LocalClient) Call(ctx context.Context, data []byte, from common.Address, evmHeight int64) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (l *LocalClient) EstimateGas(ctx context.Context, data []byte, from common.Address, evmHeight int64) (uint64, error) {
	//TODO implement me
	panic("implement me")
}

func (l *LocalClient) GetNonce(ctx context.Context, address common.Address, evmHeight int64) (uint64, error) {
	return l.state.GetNonce(address), nil
}

func (l *LocalClient) GetCode(ctx context.Context, address common.Address, evmHeight int64) ([]byte, error) {
	return l.state.GetCode(address), nil
}

func (l *LocalClient) GetStorageAt(ctx context.Context, address common.Address, hash common.Hash, evmHeight int64) (common.Hash, error) {
	return l.state.GetState(address, hash), nil
}

func (l *LocalClient) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	return l.blocks.LatestEVMHeight()
}
