package requester

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/cadence"
	evmImpl "github.com/onflow/flow-go/fvm/evm/impl"
	evmPrecompiles "github.com/onflow/flow-go/fvm/evm/precompiles"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

const cadenceArchCallGasLimit = 155_000

var (
	//go:embed cadence/dry_run.cdc
	dryRunScript []byte
)

var cadenceArchAddress = gethCommon.HexToAddress("0x0000000000000000000000010000000000000001")

type RemoteCadenceArch struct {
	blockHeight uint64
	client      *CrossSporkClient
	chainID     flow.ChainID
	cachedCalls map[string]evmTypes.Data
}

var _ evmTypes.PrecompiledContract = (*RemoteCadenceArch)(nil)

func NewRemoteCadenceArch(
	blockHeight uint64,
	client *CrossSporkClient,
	chainID flow.ChainID,
) *RemoteCadenceArch {
	return &RemoteCadenceArch{
		blockHeight: blockHeight,
		client:      client,
		chainID:     chainID,
		cachedCalls: map[string]evmTypes.Data{},
	}
}

func (rca *RemoteCadenceArch) Name() string {
	return evmPrecompiles.CADENCE_ARCH_PRECOMPILE_NAME
}

func (rca *RemoteCadenceArch) Address() evmTypes.Address {
	return evmTypes.NewAddress(cadenceArchAddress)
}

func (rca *RemoteCadenceArch) RequiredGas(input []byte) uint64 {
	evmResult, err := rca.runCall(input)
	if err != nil {
		return 0
	}

	return evmResult.GasConsumed
}

func (rca *RemoteCadenceArch) Run(input []byte) ([]byte, error) {
	key := hex.EncodeToString(crypto.Keccak256(input))

	if result, ok := rca.cachedCalls[key]; ok {
		return result, nil
	}

	evmResult, err := rca.runCall(input)
	if err != nil {
		return nil, err
	}
	return evmResult.ReturnedData, nil
}

func (rca *RemoteCadenceArch) runCall(input []byte) (*evmTypes.ResultSummary, error) {
	tx := types.NewTx(
		&types.LegacyTx{
			Nonce:    0,
			To:       &cadenceArchAddress,
			Value:    big.NewInt(0),
			Gas:      cadenceArchCallGasLimit,
			GasPrice: big.NewInt(0),
			Data:     input,
		},
	)
	encodedTx, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(encodedTx))
	if err != nil {
		return nil, err
	}

	hexEncodedAddress, err := cadence.NewString(evmTypes.CoinbaseAddress.String())
	if err != nil {
		return nil, err
	}

	scriptResult, err := rca.client.ExecuteScriptAtBlockHeight(
		context.Background(),
		rca.blockHeight,
		replaceAddresses(dryRunScript, rca.chainID),
		[]cadence.Value{hexEncodedTx, hexEncodedAddress},
	)
	if err != nil {
		return nil, err
	}

	evmResult, err := parseResult(scriptResult)
	if err != nil {
		return nil, err
	}

	key := hex.EncodeToString(crypto.Keccak256(input))
	rca.cachedCalls[key] = evmResult.ReturnedData

	return evmResult, nil
}

func parseResult(res cadence.Value) (*evmTypes.ResultSummary, error) {
	result, err := evmImpl.ResultSummaryFromEVMResultValue(res)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EVM result of type: %s, with: %w", res.Type().ID(), err)
	}

	if result.ErrorCode != 0 {
		if result.ErrorCode == evmTypes.ExecutionErrCodeExecutionReverted {
			return nil, errs.NewRevertError(result.ReturnedData)
		}
		return nil, errs.NewFailedTransactionError(result.ErrorMessage)
	}

	return result, nil
}
