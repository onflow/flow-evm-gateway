package models

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
)

const feeParamsPrecision = 100_000_000

var surgeFactorScale = big.NewInt(feeParamsPrecision)

func DefaultFeeParameters() *FeeParameters {
	return &FeeParameters{
		SurgeFactor:         cadence.UFix64(feeParamsPrecision),
		InclusionEffortCost: cadence.UFix64(feeParamsPrecision),
		ExecutionEffortCost: cadence.UFix64(feeParamsPrecision),
	}
}

type FeeParameters struct {
	SurgeFactor         cadence.UFix64 `cadence:"surgeFactor"`
	InclusionEffortCost cadence.UFix64 `cadence:"inclusionEffortCost"`
	ExecutionEffortCost cadence.UFix64 `cadence:"executionEffortCost"`
}

func (f *FeeParameters) ToBytes() ([]byte, error) {
	return rlp.EncodeToBytes(f)
}

func (f *FeeParameters) CalculateGasPrice(currentGasPrice *big.Int) *big.Int {
	if currentGasPrice == nil {
		return new(big.Int) // zero
	}

	// gasPrice = (currentGasPrice * surgeFactor) / feeParamsPrecision
	surgeFactor := new(big.Int).SetUint64(uint64(f.SurgeFactor))
	gasPrice := new(big.Int).Mul(currentGasPrice, surgeFactor)
	return new(big.Int).Quo(gasPrice, surgeFactorScale)
}

func NewFeeParametersFromBytes(data []byte) (*FeeParameters, error) {
	feeParameters := &FeeParameters{}
	if err := rlp.DecodeBytes(data, feeParameters); err != nil {
		return nil, err
	}

	return feeParameters, nil
}

func decodeFeeParametersChangedEvent(event cadence.Event) (*FeeParameters, error) {
	feeParameters := &FeeParameters{}
	if err := cadence.DecodeFields(event, feeParameters); err != nil {
		return nil, fmt.Errorf(
			"failed to Cadence-decode FlowFees.FeeParametersChanged event [%s]: %w",
			event.String(),
			err,
		)
	}

	return feeParameters, nil
}
