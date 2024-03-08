package models

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var _ FlowEVMTxData = &GethTx{}

type GethTx struct {
	*types.Transaction
}

func (gt GethTx) Hash() (common.Hash, error) {
	return gt.Transaction.Hash(), nil
}

func (gt GethTx) From() (common.Address, error) {
	return types.Sender(
		types.LatestSignerForChainID(gt.ChainId()),
		gt.Transaction,
	)
}

func (gt GethTx) MarshalBinary() ([]byte, error) {
	encoded, err := gt.Transaction.MarshalBinary()
	return append([]byte{gt.Type()}, encoded...), err
}
