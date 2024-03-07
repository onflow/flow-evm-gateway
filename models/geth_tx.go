package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
)

type GethTx struct {
	Tx *gethTypes.Transaction
}

func (gt GethTx) Hash() common.Hash {
	return gt.Tx.Hash()
}

func (gt GethTx) RawSignatureValues() (v *big.Int, r *big.Int, s *big.Int) {
	return gt.Tx.RawSignatureValues()
}

func (gt GethTx) From() common.Address {
	from, _ := gethTypes.Sender(gethTypes.LatestSignerForChainID(gt.Tx.ChainId()), gt.Tx)
	return from
}

func (gt GethTx) To() *common.Address {
	return gt.Tx.To()
}

func (gt GethTx) Data() []byte {
	return gt.Tx.Data()
}

func (gt GethTx) Nonce() uint64 {
	return gt.Tx.Nonce()
}

func (gt GethTx) Value() *big.Int {
	return gt.Tx.Value()
}

func (gt GethTx) Type() uint8 {
	return gt.Tx.Type()
}

func (gt GethTx) Gas() uint64 {
	return gt.Tx.Gas()
}

func (gt GethTx) GasPrice() *big.Int {
	return gt.Tx.GasPrice()
}

func (gt GethTx) BlobGas() uint64 {
	return gt.Tx.BlobGas()
}

func (gt GethTx) Size() uint64 {
	return gt.Tx.Size()
}

func (gt GethTx) MarshalBinary() ([]byte, error) {
	encoded, err := gt.Tx.MarshalBinary()
	return append([]byte{gt.Tx.Type()}, encoded...), err
}
