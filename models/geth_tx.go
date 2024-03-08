package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
)


var _ FlowEVMTxData = &GethTx{}

type GethTx struct {
	*gethTypes.Transaction
}

func (gt *GethTx) From() (common.Address, error) {
    return gethTypes.Sender(gethTypes.LatestSignerForChainID(gt.ChainId()), gt.Transaction)
}

func (gt *GethTx) MarshalBinary() ([]byte, error) {
	encoded, err := gt.MarshalBinary()
	return append([]byte{gt.Type()}, encoded...), err
}

