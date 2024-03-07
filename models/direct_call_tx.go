package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm/types"
)

type DirectCallTx struct {
	Call *types.DirectCall
}

func (dc DirectCallTx) Hash() common.Hash {
	hash, _ := dc.Call.Hash()
	return hash
}

func (dc DirectCallTx) RawSignatureValues() (v *big.Int, r *big.Int, s *big.Int) {
	return big.NewInt(0), big.NewInt(0), big.NewInt(0)
}

func (dc DirectCallTx) From() common.Address {
	return dc.Call.From.ToCommon()
}

func (dc DirectCallTx) To() *common.Address {
	var to *common.Address
	if !dc.Call.EmptyToField() {
		ct := dc.Call.To.ToCommon()
		to = &ct
	}
	return to
}

func (dc DirectCallTx) Data() []byte {
	return dc.Call.Data
}

func (dc DirectCallTx) Nonce() uint64 {
	return dc.Call.Nonce
}

func (dc DirectCallTx) Value() *big.Int {
	return dc.Call.Value
}

func (dc DirectCallTx) Type() uint8 {
	return dc.Call.Type
}

func (dc DirectCallTx) Gas() uint64 {
	return dc.Call.GasLimit
}

func (dc DirectCallTx) GasPrice() *big.Int {
	return dc.Call.Transaction().GasPrice()
}

func (dc DirectCallTx) BlobGas() uint64 {
	return dc.Call.Transaction().BlobGas()
}

func (dc DirectCallTx) Size() uint64 {
	return dc.Call.Transaction().Size()
}

func (dc DirectCallTx) MarshalBinary() ([]byte, error) {
	return dc.Call.Encode()
}
