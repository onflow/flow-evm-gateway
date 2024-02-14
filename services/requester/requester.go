package requester

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
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

	// Call executes the given signed transaction data on the state for the given block number.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(ctx context.Context, address common.Address, data []byte) ([]byte, error)
}

var _ Requester = &EVM{}

type EVM struct {
	client  access.Client
	address flow.Address
	signer  crypto.Signer
}

func (e *EVM) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	tx := &types.Transaction{}
	err := tx.DecodeRLP(
		rlp.NewStream(
			bytes.NewReader(data),
			uint64(len(data)),
		),
	)

	latestBlock, err := e.client.GetLatestBlock(ctx, true)
	if err != nil {
		return common.Hash{}, err
	}

	index, seqNum, err := e.getSignerNetworkInfo(ctx)
	if err != nil {
		return common.Hash{}, err
	}

	flowTx := flow.NewTransaction().
		SetScript(runTxScript).
		SetProposalKey(e.address, index, seqNum).
		SetReferenceBlockID(latestBlock.ID).
		SetPayer(e.address).
		AddAuthorizer(e.address)

	if err = flowTx.AddArgument(cadenceArrayFromBytes(data)); err != nil {
		return common.Hash{}, err
	}

	if err = flowTx.SignEnvelope(e.address, index, e.signer); err != nil {
		return common.Hash{}, err
	}

	err = e.client.SendTransaction(ctx, *flowTx)
	if err != nil {
		return common.Hash{}, err
	}

	return tx.Hash(), nil
}

func (e *EVM) GetBalance(ctx context.Context, address common.Address, height uint64) (*big.Int, error) {
	// todo make sure provided height is used
	addr := cadenceArrayFromBytes(address.Bytes()).WithType(addressType)

	val, err := e.executeScript(ctx, getBalanceScript, []cadence.Value{addr})
	if err != nil {
		return nil, err
	}

	return new(big.Int).SetBytes(val), nil
}

func (e *EVM) Call(ctx context.Context, address common.Address, data []byte) ([]byte, error) {
	txData := cadenceArrayFromBytes(data).WithType(byteArrayType)
	toAddress := cadenceArrayFromBytes(address.Bytes()).WithType(addressType)

	return e.executeScript(ctx, callScript, []cadence.Value{txData, toAddress})
}

func (e *EVM) executeScript(ctx context.Context, script []byte, args []cadence.Value) ([]byte, error) {
	value, err := e.client.ExecuteScriptAtLatestBlock(ctx, script, args)
	if err != nil {
		return nil, err
	}

	return bytesFromCadenceArray(value)
}

// getSignerNetworkInfo loads the signer account from network and returns key index and sequence number
func (e *EVM) getSignerNetworkInfo(ctx context.Context) (int, uint64, error) {
	account, err := e.client.GetAccount(ctx, e.address)
	if err != nil {
		return 0, 0, err
	}

	signerPub := e.signer.PublicKey()
	for _, k := range account.Keys {
		if k.PublicKey.Equals(signerPub) {
			return k.Index, k.SequenceNumber, nil
		}
	}

	return 0, 0, fmt.Errorf("provided account address and signer keys do not match")
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
