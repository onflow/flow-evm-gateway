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
	logger = logger.With().Str("component", "requester").Logger()
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
		return nil, fmt.Errorf("COA account must be funded with at least %d Flow", minFlowBalance)
	}

	evm := &EVM{
		client:  client,
		address: address,
		signer:  signer,
		logger:  logger,
	}

	// create COA on the account
	// todo improve this to first check if coa exists and only if it doesn't create it, if it doesn't and the flag is false return an error
	if createCOA {
		// we ignore errors for now since creation of already existing COA resource will fail, which is fine for now
		id, err := evm.signAndSend(context.Background(), createCOAScript, cadence.UFix64(coaFundingBalance))
		logger.Info().Err(err).Str("id", id.String()).Msg("COA resource auto-created")
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

	// todo make sure the gas price is not bellow the configured gas price

	flowID, err := e.signAndSend(ctx, runTxScript, cadenceArrayFromBytes(data))
	if err != nil {
		return common.Hash{}, err
	}

	var to string
	if tx.To() != nil {
		to = tx.To().String()
	}
	e.logger.Info().
		Str("evm ID", tx.Hash().Hex()).
		Str("flow ID", flowID.Hex()).
		Str("to", to).
		Str("value", tx.Value().String()).
		Str("data", fmt.Sprintf("%x", tx.Data())).
		Msg("raw transaction submitted")

	return tx.Hash(), nil
}

// signAndSend creates a flow transaction from the provided script with the arguments and signs it with the
// configured COA account and then submits it to the network.
func (e *EVM) signAndSend(ctx context.Context, script []byte, args ...cadence.Value) (flow.Identifier, error) {
	latestBlock, err := e.client.GetLatestBlock(ctx, true)
	if err != nil {
		return flow.EmptyID, fmt.Errorf("failed to get latest flow block: %w", err)
	}

	index, seqNum, err := e.getSignerNetworkInfo(ctx)
	if err != nil {
		return flow.EmptyID, fmt.Errorf("failed to get signer info: %w", err)
	}

	flowTx := flow.NewTransaction().
		SetScript(script).
		SetProposalKey(e.address, index, seqNum).
		SetReferenceBlockID(latestBlock.ID).
		SetPayer(e.address).
		AddAuthorizer(e.address)

	for _, arg := range args {
		if err = flowTx.AddArgument(arg); err != nil {
			return flow.EmptyID, fmt.Errorf("failed to add argument: %w", err)
		}
	}

	if err = flowTx.SignEnvelope(e.address, index, e.signer); err != nil {
		return flow.EmptyID, fmt.Errorf("failed to sign envelope: %w", err)
	}

	err = e.client.SendTransaction(ctx, *flowTx)
	if err != nil {
		return flow.EmptyID, fmt.Errorf("failed to send transaction: %w", err)
	}

	// todo should we wait for the transaction result?
	// we should handle a case where flow transaction is failed but we will get a result back, it would only be failed,
	// but there is no evm transaction. So if we submit an evm tx and get back an ID and then we wait for receipt
	// we would never get it, but this failure of sending flow transaction could somehow help with this case

	return flowTx.ID(), nil
}

func (e *EVM) GetBalance(ctx context.Context, address common.Address, height uint64) (*big.Int, error) {
	// todo make sure provided height is used
	addr := cadenceArrayFromBytes(address.Bytes()).WithType(addressType)

	val, err := e.client.ExecuteScriptAtLatestBlock(ctx, getBalanceScript, []cadence.Value{addr})
	if err != nil {
		return nil, err
	}

	e.logger.Info().Str("address", address.String()).Msg("get balance")

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt); !ok {
		e.logger.Panic().Msg(fmt.Sprintf("failed to convert balance %v to UInt", val))
	}

	return val.(cadence.UInt).ToGoValue().(*big.Int), nil
}

func (e *EVM) Call(ctx context.Context, address common.Address, data []byte) ([]byte, error) {
	// todo make "to" address optional, this can be used for contract deployment simulations
	txData := cadenceArrayFromBytes(data).WithType(byteArrayType)
	toAddress := cadenceArrayFromBytes(address.Bytes()).WithType(addressType)

	e.logger.Info().
		Str("address", address.Hex()).
		Str("data", string(data)).
		Msg("call")

	value, err := e.client.ExecuteScriptAtLatestBlock(ctx, callScript, []cadence.Value{txData, toAddress})
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
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
