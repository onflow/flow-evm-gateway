package requester

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/crypto"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/go-ethereum/common"
	gethCore "github.com/onflow/go-ethereum/core"
	"github.com/onflow/go-ethereum/core/types"
	gethVM "github.com/onflow/go-ethereum/core/vm"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

var (
	//go:embed cadence/call.cdc
	callScript []byte

	//go:embed cadence/run.cdc
	runTxScript []byte

	//go:embed cadence/get_balance.cdc
	getBalanceScript []byte

	//go:embed cadence/create_coa.cdc
	createCOAScript []byte

	//go:embed cadence/estimate_gas.cdc
	estimateGasScript []byte

	//go:embed cadence/get_nonce.cdc
	getNonceScript []byte

	//go:embed cadence/get_code.cdc
	getCodeScript []byte
)

const minFlowBalance = 2
const coaFundingBalance = minFlowBalance - 1

type Requester interface {
	// SendRawTransaction will submit signed transaction data to the network.
	// The submitted EVM transaction hash is returned.
	SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error)

	// GetBalance returns the amount of wei for the given address in the state of the
	// given block height.
	GetBalance(ctx context.Context, address common.Address, height uint64) (*big.Int, error)

	// Call executes the given signed transaction data on the state for the given block number.
	// Note, this function doesn't make and changes in the state/blockchain and is
	// useful to execute and retrieve values.
	Call(ctx context.Context, data []byte) ([]byte, error)

	// EstimateGas executes the given signed transaction data on the state.
	// Note, this function doesn't make any changes in the state/blockchain and is
	// useful to executed and retrieve the gas consumption and possible failures.
	EstimateGas(ctx context.Context, data []byte) (uint64, error)

	// GetNonce gets nonce from the network.
	GetNonce(ctx context.Context, address common.Address) (uint64, error)

	// GetCode returns the code stored at the given address in
	// the state for the given block number.
	GetCode(ctx context.Context, address common.Address, height uint64) ([]byte, error)
}

var _ Requester = &EVM{}

type EVM struct {
	client access.Client
	config *config.Config
	signer crypto.Signer
	logger zerolog.Logger
}

func NewEVM(
	client access.Client,
	config *config.Config,
	signer crypto.Signer,
	logger zerolog.Logger,
) (*EVM, error) {
	logger = logger.With().Str("component", "requester").Logger()
	// check that the address stores already created COA resource in the "evm" storage path.
	// if it doesn't check if the auto-creation boolean is true and if so create it
	// otherwise fail. COA resource is required by the EVM requester to be able to submit transactions.
	address := config.COAAddress
	acc, err := client.GetAccount(context.Background(), address)
	if err != nil {
		return nil, fmt.Errorf(
			"could not fetch the configured COA account: %s make sure it exists: %w",
			address.String(),
			err,
		)
	}

	if acc.Balance < minFlowBalance {
		return nil, fmt.Errorf(
			"COA account must be funded with at least %d Flow, but has balance of: %d",
			minFlowBalance,
			acc.Balance,
		)
	}

	evm := &EVM{
		client: client,
		config: config,
		signer: signer,
		logger: logger,
	}

	// create COA on the account
	if config.CreateCOAResource {
		// we ignore errors for now since creation of already existing COA resource will fail, which is fine for now
		id, err := evm.signAndSend(
			context.Background(),
			evm.replaceAddresses(createCOAScript),
			cadence.UFix64(coaFundingBalance),
		)
		logger.Warn().Err(err).Str("id", id.String()).Msg("COA resource auto-creation status")
	}

	return evm, nil
}

func (e *EVM) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	e.logger.Debug().
		Str("data", fmt.Sprintf("%x", data)).
		Msg("send raw transaction")

	tx := &types.Transaction{}
	if err := tx.UnmarshalBinary(data); err != nil {
		return common.Hash{}, err
	}

	if tx.GasPrice().Cmp(e.config.GasPrice) < 0 {
		return common.Hash{}, errors.NewErrGasPriceTooLow(e.config.GasPrice)
	}

	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(data))
	if err != nil {
		return common.Hash{}, err
	}

	// todo make sure the gas price is not bellow the configured gas price
	script := e.replaceAddresses(runTxScript)
	flowID, err := e.signAndSend(ctx, script, hexEncodedTx)
	if err != nil {
		return common.Hash{}, err
	}

	var to string
	if tx.To() != nil {
		to = tx.To().String()
	}

	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to calculate sender")
	}

	e.logger.Info().
		Str("evm-id", tx.Hash().Hex()).
		Str("flow-id", flowID.Hex()).
		Str("to", to).
		Str("from", from.Hex()).
		Str("value", tx.Value().String()).
		Msg("raw transaction sent")

	return tx.Hash(), nil
}

// signAndSend creates a flow transaction from the provided script with the arguments and signs it with the
// configured COA account and then submits it to the network.
func (e *EVM) signAndSend(ctx context.Context, script []byte, args ...cadence.Value) (flow.Identifier, error) {
	var (
		g           = errgroup.Group{}
		err1, err2  error
		latestBlock *flow.Block
		index       int
		seqNum      uint64
	)
	// execute concurrently so we can speed up all the information we need for tx
	g.Go(func() error {
		latestBlock, err1 = e.client.GetLatestBlock(ctx, true)
		return err1
	})
	g.Go(func() error {
		index, seqNum, err2 = e.getSignerNetworkInfo(ctx)
		return err2
	})
	if err := g.Wait(); err != nil {
		return flow.EmptyID, err
	}

	address := e.config.COAAddress
	flowTx := flow.NewTransaction().
		SetScript(script).
		SetProposalKey(address, index, seqNum).
		SetReferenceBlockID(latestBlock.ID).
		SetPayer(address).
		AddAuthorizer(address)

	for _, arg := range args {
		if err := flowTx.AddArgument(arg); err != nil {
			return flow.EmptyID, fmt.Errorf("failed to add argument: %w", err)
		}
	}

	if err := flowTx.SignEnvelope(address, index, e.signer); err != nil {
		return flow.EmptyID, fmt.Errorf("failed to sign envelope: %w", err)
	}

	if err := e.client.SendTransaction(ctx, *flowTx); err != nil {
		return flow.EmptyID, fmt.Errorf("failed to send transaction: %w", err)
	}

	// this is only used for debugging purposes
	if d := e.logger.Debug(); d.Enabled() {
		go func(id flow.Identifier) {
			res, _ := e.client.GetTransactionResult(context.Background(), id)
			if res != nil && res.Error != nil {
				e.logger.Error().
					Str("flow-id", id.String()).
					Err(res.Error).
					Msg("flow transaction failed to execute")
				return
			}

			e.logger.Debug().
				Str("flow-id", id.String()).
				Str("events", fmt.Sprintf("%v", res.Events)).
				Msg("flow transaction executed successfully")
		}(flowTx.ID())
	}

	return flowTx.ID(), nil
}

func (e *EVM) GetBalance(ctx context.Context, address common.Address, height uint64) (*big.Int, error) {
	// todo make sure provided height is used
	hexEncodedAddress, err := addressToCadenceString(address)
	if err != nil {
		return nil, err
	}

	val, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		e.replaceAddresses(getBalanceScript),
		[]cadence.Value{hexEncodedAddress},
	)
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

func (e *EVM) GetNonce(ctx context.Context, address common.Address) (uint64, error) {
	hexEncodedAddress, err := addressToCadenceString(address)
	if err != nil {
		return 0, err
	}

	val, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		e.replaceAddresses(getNonceScript),
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		return 0, err
	}

	e.logger.Info().Str("address", address.String()).Msg("get nonce")

	// sanity check, should never occur
	if _, ok := val.(cadence.UInt64); !ok {
		e.logger.Panic().Msg(fmt.Sprintf("failed to convert balance %v to UInt64", val))
	}

	return val.(cadence.UInt64).ToGoValue().(uint64), nil
}

func (e *EVM) Call(ctx context.Context, data []byte) ([]byte, error) {
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(data))
	if err != nil {
		return nil, err
	}

	e.logger.Debug().
		Str("data", fmt.Sprintf("%x", data)).
		Msg("call")

	scriptResult, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		e.replaceAddresses(callScript),
		[]cadence.Value{hexEncodedTx},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	output, err := cadenceStringToBytes(scriptResult)
	if err != nil {
		return nil, err
	}

	e.logger.Info().
		Str("data", fmt.Sprintf("%x", data)).
		Str("result", hex.EncodeToString(output)).
		Msg("call executed")

	return output, nil
}

func (e *EVM) EstimateGas(ctx context.Context, data []byte) (uint64, error) {
	e.logger.Debug().
		Str("data", fmt.Sprintf("%x", data)).
		Msg("estimate gas")

	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(data))
	if err != nil {
		return 0, err
	}

	value, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		e.replaceAddresses(estimateGasScript),
		[]cadence.Value{hexEncodedTx},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to execute script: %w", err)
	}

	// sanity check, should never occur
	// TODO(m-Peter): Consider adding a decoder for EVM.Result struct
	// to a Go value/type.
	if _, ok := value.(cadence.Array); !ok {
		e.logger.Panic().Msg(fmt.Sprintf("failed to convert value to array: %v", value))
	}

	result := value.(cadence.Array)
	errorCode := result.Values[0].ToGoValue().(uint64)

	if errorCode != 0 {
		return 0, getErrorForCode(errorCode)
	}

	gasUsed := result.Values[1].ToGoValue().(uint64)
	return gasUsed, nil
}

func (e *EVM) GetCode(
	ctx context.Context,
	address common.Address,
	height uint64,
) ([]byte, error) {
	e.logger.Debug().
		Str("address", address.Hex()).
		Uint64("height", height).
		Msg("get code")

	hexEncodedAddress, err := addressToCadenceString(address)
	if err != nil {
		return nil, err
	}

	value, err := e.client.ExecuteScriptAtLatestBlock(
		ctx,
		e.replaceAddresses(getCodeScript),
		[]cadence.Value{hexEncodedAddress},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute script for get code: %w", err)
	}

	code, err := cadenceStringToBytes(value)
	if err != nil {
		return nil, err
	}

	e.logger.Info().
		Str("address", address.Hex()).
		Str("code size", fmt.Sprintf("%d", len(code))).
		Msg("get code executed")

	return code, nil
}

// getSignerNetworkInfo loads the signer account from network and returns key index and sequence number
func (e *EVM) getSignerNetworkInfo(ctx context.Context) (int, uint64, error) {
	account, err := e.client.GetAccount(ctx, e.config.COAAddress)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get signer info account: %w", err)
	}

	signerPub := e.signer.PublicKey()
	for _, k := range account.Keys {
		if k.PublicKey.Equals(signerPub) {
			return k.Index, k.SequenceNumber, nil
		}
	}

	return 0, 0, fmt.Errorf("provided account address and signer keys do not match")
}

// replaceAddresses replace the addresses based on the network
func (e *EVM) replaceAddresses(script []byte) []byte {
	// make the list of all contracts we should replace address for
	sc := systemcontracts.SystemContractsForChain(e.config.FlowNetworkID)
	contracts := []systemcontracts.SystemContract{sc.EVMContract, sc.FungibleToken, sc.FlowToken}

	s := string(script)
	// iterate over all the import name and address pairs and replace them in script
	for _, contract := range contracts {
		s = strings.ReplaceAll(s,
			fmt.Sprintf("import %s", contract.Name),
			fmt.Sprintf("import %s from %s", contract.Name, contract.Address.HexWithPrefix()),
		)
	}

	// also replace COA address if used (in scripts)
	s = strings.ReplaceAll(s, "0xCOA", e.config.COAAddress.HexWithPrefix())

	return []byte(s)
}

func addressToCadenceString(address common.Address) (cadence.String, error) {
	return cadence.NewString(
		strings.TrimPrefix(address.Hex(), "0x"),
	)
}

func cadenceStringToBytes(value cadence.Value) ([]byte, error) {
	cdcString, ok := value.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("failed to convert cadence value to string: %v", value)
	}

	hexEncodedCode := cdcString.ToGoValue().(string)
	code, err := hex.DecodeString(hexEncodedCode)
	if err != nil {
		return nil, fmt.Errorf("failed to decode string to byte array: %w", err)
	}

	return code, nil
}

// TODO(m-Peter): Consider moving this to flow-go repository
func getErrorForCode(errorCode uint64) error {
	switch evmTypes.ErrorCode(errorCode) {
	case evmTypes.ValidationErrCodeGasUintOverflow:
		return gethVM.ErrGasUintOverflow
	case evmTypes.ValidationErrCodeNonceTooLow:
		return gethCore.ErrNonceTooLow
	case evmTypes.ValidationErrCodeNonceTooHigh:
		return gethCore.ErrNonceTooHigh
	case evmTypes.ValidationErrCodeNonceMax:
		return gethCore.ErrNonceMax
	case evmTypes.ValidationErrCodeGasLimitReached:
		return gethCore.ErrGasLimitReached
	case evmTypes.ValidationErrCodeInsufficientFundsForTransfer:
		return gethCore.ErrInsufficientFundsForTransfer
	case evmTypes.ValidationErrCodeMaxInitCodeSizeExceeded:
		return gethCore.ErrMaxInitCodeSizeExceeded
	case evmTypes.ValidationErrCodeInsufficientFunds:
		return gethCore.ErrInsufficientFunds
	case evmTypes.ValidationErrCodeIntrinsicGas:
		return gethCore.ErrIntrinsicGas
	case evmTypes.ValidationErrCodeTxTypeNotSupported:
		return gethCore.ErrTxTypeNotSupported
	case evmTypes.ValidationErrCodeTipAboveFeeCap:
		return gethCore.ErrTipAboveFeeCap
	case evmTypes.ValidationErrCodeTipVeryHigh:
		return gethCore.ErrTipVeryHigh
	case evmTypes.ValidationErrCodeFeeCapVeryHigh:
		return gethCore.ErrFeeCapVeryHigh
	case evmTypes.ValidationErrCodeFeeCapTooLow:
		return gethCore.ErrFeeCapTooLow
	case evmTypes.ValidationErrCodeSenderNoEOA:
		return gethCore.ErrSenderNoEOA
	case evmTypes.ValidationErrCodeBlobFeeCapTooLow:
		return gethCore.ErrBlobFeeCapTooLow
	case evmTypes.ExecutionErrCodeOutOfGas:
		return gethVM.ErrOutOfGas
	case evmTypes.ExecutionErrCodeCodeStoreOutOfGas:
		return gethVM.ErrCodeStoreOutOfGas
	case evmTypes.ExecutionErrCodeDepth:
		return gethVM.ErrDepth
	case evmTypes.ExecutionErrCodeInsufficientBalance:
		return gethVM.ErrInsufficientBalance
	case evmTypes.ExecutionErrCodeContractAddressCollision:
		return gethVM.ErrContractAddressCollision
	case evmTypes.ExecutionErrCodeExecutionReverted:
		return gethVM.ErrExecutionReverted
	case evmTypes.ExecutionErrCodeMaxInitCodeSizeExceeded:
		return gethVM.ErrMaxInitCodeSizeExceeded
	case evmTypes.ExecutionErrCodeMaxCodeSizeExceeded:
		return gethVM.ErrMaxCodeSizeExceeded
	case evmTypes.ExecutionErrCodeInvalidJump:
		return gethVM.ErrInvalidJump
	case evmTypes.ExecutionErrCodeWriteProtection:
		return gethVM.ErrWriteProtection
	case evmTypes.ExecutionErrCodeReturnDataOutOfBounds:
		return gethVM.ErrReturnDataOutOfBounds
	case evmTypes.ExecutionErrCodeGasUintOverflow:
		return gethVM.ErrGasUintOverflow
	case evmTypes.ExecutionErrCodeInvalidCode:
		return gethVM.ErrInvalidCode
	case evmTypes.ExecutionErrCodeNonceUintOverflow:
		return gethVM.ErrNonceUintOverflow
	case evmTypes.ValidationErrCodeMisc:
		return fmt.Errorf("validation error: %d", errorCode)
	case evmTypes.ExecutionErrCodeMisc:
		return fmt.Errorf("execution error: %d", errorCode)
	}

	return fmt.Errorf("unknown error code: %d", errorCode)
}
