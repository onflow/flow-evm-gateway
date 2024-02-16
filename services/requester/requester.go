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
	"github.com/rs/zerolog"
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

	//go:embed cadence/create_bridged_account.cdc
	createCOAScript []byte

	byteArrayType = cadence.NewVariableSizedArrayType(cadence.UInt8Type)
	addressType   = cadence.NewConstantSizedArrayType(
		common.AddressLength,
		cadence.UInt8Type,
	)
)

const minFlowBalance = 2
const coaFundingBalance = minFlowBalance - 1

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
	logger  zerolog.Logger
	client  access.Client
	address flow.Address
	signer  crypto.Signer
}

func NewEVM(
	client access.Client,
	address flow.Address,
	signer crypto.Signer,
	createCOA bool,
	logger zerolog.Logger,
) (*EVM, error) {
	// check that the address stores already created COA resource in the "evm" storage path.
	// if it doesn't check if the auto-creation boolean is true and if so create it
	// otherwise fail. COA resource is required by the EVM requester to be able to submit transactions.
	acc, err := client.GetAccount(context.Background(), address)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch the configured COA account: %s make sure it exists: %w",
			address.String(),
			err,
		)
	}

	if acc.Balance < minFlowBalance {
		return nil, fmt.Errorf("COA account must be funded with at least %f Flow", minFlowBalance)
	}

	evm := &EVM{
		client:  client,
		address: address,
		signer:  signer,
	}

	// create COA on the account
	// todo improve this to first check if coa exists and only if it doesn't create it, if it doesn't and the flag is false return an error
	if createCOA {
		// we ignore errors for now since creation of already existing COA resource will fail, which is fine for now
		_, _ = evm.signAndSend(context.Background(), createCOAScript, cadence.UFix64(coaFundingBalance))
	}

	return evm, nil
}

func (e *EVM) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	tx := &types.Transaction{}
	err := tx.DecodeRLP(
		rlp.NewStream(
			bytes.NewReader(data),
			uint64(len(data)),
		),
	)

	_, err = e.signAndSend(ctx, runTxScript, cadenceArrayFromBytes(data))
	if err != nil {
		return common.Hash{}, err
	}

	return tx.Hash(), nil
}

// signAndSend creates a flow transaction from the provided script with the arguments and signs it with the
// configured COA account and then submits it to the network.
func (e *EVM) signAndSend(ctx context.Context, script []byte, args ...cadence.Value) (flow.Identifier, error) {
	latestBlock, err := e.client.GetLatestBlock(ctx, true)
	if err != nil {
		return flow.EmptyID, err
	}

	index, seqNum, err := e.getSignerNetworkInfo(ctx)
	if err != nil {
		return flow.EmptyID, err
	}

	flowTx := flow.NewTransaction().
		SetScript(script).
		SetProposalKey(e.address, index, seqNum).
		SetReferenceBlockID(latestBlock.ID).
		SetPayer(e.address).
		AddAuthorizer(e.address)

	for _, arg := range args {
		if err = flowTx.AddArgument(arg); err != nil {
			return flow.EmptyID, err
		}
	}

	if err = flowTx.SignEnvelope(e.address, index, e.signer); err != nil {
		return flow.EmptyID, err
	}

	err = e.client.SendTransaction(ctx, *flowTx)
	if err != nil {
		return flow.EmptyID, err
	}

	return flowTx.ID(), nil
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

	// todo support key rotation
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
