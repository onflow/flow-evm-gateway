package requester

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk/access"
)

var (
	//go:embed cadence/bridged_account_call.cdc
	callScript []byte

	//go:embed cadence/evm_run.cdc
	runTxScript []byte

	//go:embed cadence/evm_address_balance.cdc
	getBalanceScript []byte

	byteArrayType = cadence.NewVariableSizedArrayType(cadence.UInt8Type{})
	addressType   = cadence.NewConstantSizedArrayType(
		common.AddressLength,
		cadence.UInt8Type{},
	)
)

type Requester interface {
	// SendRawTransaction will submit signed transaction data to the network.
	// The submitted EVM transaction hash is returned.
	SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error)

	// GetBalance returns the amount of wei for the given address in the state of the
	// given block height.
	// todo in future this should be deprecated for local data
	GetBalance(ctx context.Context, address common.Address, height uint64) (*big.Int, error)

	// GetNonce returns the nonce of the account.
	// todo in future this should be deprecated for local data
	GetNonce(ctx context.Context, address common.Address) (uint64, error)

	// Call executes the given signed transaction data on the state for the given block number.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(ctx context.Context, address common.Address, data []byte) ([]byte, error)
}

var _ Requester = &EVM{}

type EVM struct {
	client access.Client
}

func (e *EVM) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	//TODO implement me
	panic("implement me")
}

func (e *EVM) GetBalance(ctx context.Context, address common.Address, height uint64) (*big.Int, error) {
	//TODO implement me
	panic("implement me")
}

func (e *EVM) GetNonce(ctx context.Context, address common.Address) (uint64, error) {
	//TODO implement me
	panic("implement me")
}

func (e *EVM) Call(ctx context.Context, address common.Address, data []byte) ([]byte, error) {

	txData := cadenceArrayFromBytes(data).WithType(byteArrayType)
	toAddress := cadenceArrayFromBytes(address.Bytes()).WithType(addressType)

	value, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		callScript,
		[]cadence.Value{txData, toAddress},
	)
	if err != nil {
		return hexutil.Bytes{}, err
	}

	return bytesFromCadenceArray(value)
}

func cadenceArrayFromBytes(input []byte) cadence.Array {
	values := make([]cadence.Value, 0)
	for _, element := range input {
		values = append(values, cadence.UInt8(element))
	}

	return cadence.NewArray(values)
}

func bytesFromCadenceArray(value cadence.Value) ([]byte, error) {
	arr, ok := value.(cadence.Array)
	if !ok {
		return nil, fmt.Errorf("cadence value is not of array type, can not conver to byte array")
	}

	res := make([]byte, len(arr.Values))
	for i, x := range arr.Values {
		res[i] = x.ToGoValue().(byte)
	}

	return res, nil
}
