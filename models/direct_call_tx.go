package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var _ FlowEVMTxData = &DirectCallTx{}

type DirectCallTx struct {
	*gethTypes.Transaction
	call *types.DirectCall
}

func (dc DirectCallTx) From() common.Address {
	return dc.call.From.ToCommon()
}

func (dc DirectCallTx) MarshalBinary() ([]byte, error) {
	return dc.call.Encode()
}

