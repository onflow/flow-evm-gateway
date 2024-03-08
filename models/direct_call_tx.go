package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var _ FlowEVMTxData = &DirectCallTx{}

type DirectCallTx struct {
	*types.DirectCall
}

func (dc DirectCallTx) RawSignatureValues() (
	v *big.Int,
	r *big.Int,
	s *big.Int,
) {
	return big.NewInt(0), big.NewInt(0), big.NewInt(0)
}

func (dc DirectCallTx) Hash() (common.Hash, error) {
	return dc.DirectCall.Hash()
}

func (dc DirectCallTx) From() (common.Address, error) {
	return dc.DirectCall.From.ToCommon(), nil
}

func (dc DirectCallTx) To() *common.Address {
	var to *common.Address
	if !dc.DirectCall.EmptyToField() {
		ct := dc.DirectCall.To.ToCommon()
		to = &ct
	}
	return to
}

func (dc DirectCallTx) Data() []byte {
	return dc.DirectCall.Data
}

func (dc DirectCallTx) Nonce() uint64 {
	return dc.DirectCall.Nonce
}

func (dc DirectCallTx) Value() *big.Int {
	return dc.DirectCall.Value
}

func (dc DirectCallTx) Type() uint8 {
	return dc.DirectCall.Type
}

func (dc DirectCallTx) Gas() uint64 {
	return dc.DirectCall.GasLimit
}

func (dc DirectCallTx) GasPrice() *big.Int {
	return dc.DirectCall.Transaction().GasPrice()
}

func (dc DirectCallTx) BlobGas() uint64 {
	return dc.DirectCall.Transaction().BlobGas()
}

func (dc DirectCallTx) Size() uint64 {
	return dc.DirectCall.Transaction().Size()
}

func (dc DirectCallTx) MarshalBinary() ([]byte, error) {
	return dc.DirectCall.Encode()
}
