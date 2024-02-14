package requester

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type Requester interface {
	// SendRawTransaction will submit signed transaction data to the network.
	// The submitted EVM transaction hash is returned.
	SendRawTransaction(data []byte) (common.Hash, error)

	// GetBalance returns the amount of wei for the given address in the state of the
	// given block height.
	GetBalance(address common.Address, height uint64) (*big.Int, error)

	// Call executes the given signed transaction data on the state for the given block number.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(address common.Address, data []byte) ([]byte, error)
}

var _ Requester = &EVM{}

type EVM struct {
}

func (e *EVM) SendRawTransaction(data []byte) (common.Hash, error) {
	//TODO implement me
	panic("implement me")
}

func (e *EVM) GetBalance(address common.Address, height uint64) (*big.Int, error) {
	//TODO implement me
	panic("implement me")
}

func (e *EVM) Call(address common.Address, data []byte) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}
