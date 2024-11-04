package requester

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/onflow/cadence"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	evmImpl "github.com/onflow/flow-go/fvm/evm/impl"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/crypto"
)

var cadenceArchAddress = gethCommon.HexToAddress("0x0000000000000000000000010000000000000001")

type RemoteCadenceArch struct {
	blockHeight uint64
	client      *CrossSporkClient
	chainID     flowGo.ChainID
	cachedCalls map[string]evmTypes.Data
}

var _ evmTypes.PrecompiledContract = (*RemoteCadenceArch)(nil)

func NewRemoteCadenceArch(
	blockHeight uint64,
	client *CrossSporkClient,
	chainID flowGo.ChainID,
) *RemoteCadenceArch {
	return &RemoteCadenceArch{
		blockHeight: blockHeight,
		client:      client,
		chainID:     chainID,
		cachedCalls: map[string]evmTypes.Data{},
	}
}

func (rca *RemoteCadenceArch) Address() evmTypes.Address {
	return evmTypes.NewAddress(cadenceArchAddress)
}

func (rca *RemoteCadenceArch) RequiredGas(input []byte) uint64 {
	evmResult, err := rca.runCall(input)
	if err != nil {
		return 0
	}

	key := hex.EncodeToString(crypto.Keccak256(input))
	rca.cachedCalls[key] = evmResult.ReturnedData

	return evmResult.GasConsumed
}

func (rca *RemoteCadenceArch) Run(input []byte) ([]byte, error) {
	key := hex.EncodeToString(crypto.Keccak256(input))
	result, ok := rca.cachedCalls[key]

	if !ok {
		evmResult, err := rca.runCall(input)
		if err != nil {
			return nil, err
		}
		return evmResult.ReturnedData, nil
	}

	return result, nil
}

func (rca *RemoteCadenceArch) replaceAddresses(script []byte) []byte {
	// make the list of all contracts we should replace address for
	sc := systemcontracts.SystemContractsForChain(rca.chainID)
	contracts := []systemcontracts.SystemContract{sc.EVMContract, sc.FungibleToken, sc.FlowToken}

	s := string(script)
	// iterate over all the import name and address pairs and replace them in script
	for _, contract := range contracts {
		s = strings.ReplaceAll(s,
			fmt.Sprintf("import %s", contract.Name),
			fmt.Sprintf("import %s from %s", contract.Name, contract.Address.HexWithPrefix()),
		)
	}

	return []byte(s)
}

func (rca *RemoteCadenceArch) runCall(input []byte) (*evmTypes.ResultSummary, error) {
	tx := types.NewTx(
		&types.LegacyTx{
			Nonce:    0,
			To:       &cadenceArchAddress,
			Value:    big.NewInt(0),
			Gas:      55_000,
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

	hexEncodedAddress, err := addressToCadenceString(evmTypes.CoinbaseAddress.ToCommon())
	if err != nil {
		return nil, err
	}

	scriptResult, err := rca.client.ExecuteScriptAtBlockHeight(
		context.Background(),
		rca.blockHeight,
		rca.replaceAddresses(dryRunScript),
		[]cadence.Value{hexEncodedTx, hexEncodedAddress},
	)
	if err != nil {
		return nil, err
	}

	evmResult, err := parseResult(scriptResult)
	if err != nil {
		return nil, err
	}

	return evmResult, nil
}

func addressToCadenceString(address gethCommon.Address) (cadence.String, error) {
	return cadence.NewString(strings.TrimPrefix(address.Hex(), "0x"))
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

	return result, err
}
