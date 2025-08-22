package models

import (
	"fmt"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
)

var DefaultFeeParameters = &FeeParameters{
	SurgeFactor:         cadence.UFix64(100_000_000),
	InclusionEffortCost: cadence.UFix64(100_000_000),
	ExecutionEffortCost: cadence.UFix64(100_000_000),
}

type FeeParameters struct {
	SurgeFactor         cadence.UFix64 `cadence:"surgeFactor"`
	InclusionEffortCost cadence.UFix64 `cadence:"inclusionEffortCost"`
	ExecutionEffortCost cadence.UFix64 `cadence:"executionEffortCost"`
}

func (f *FeeParameters) ToBytes() ([]byte, error) {
	return rlp.EncodeToBytes(f)
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
