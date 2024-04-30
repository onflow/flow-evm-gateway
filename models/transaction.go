package models

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
)

type Transaction interface {
	// TODO(m-Peter): Remove the error return value once flow-go is updated
	Hash() (common.Hash, error)
	RawSignatureValues() (v *big.Int, r *big.Int, s *big.Int)
	From() (common.Address, error)
	To() *common.Address
	Data() []byte
	Nonce() uint64
	Value() *big.Int
	Type() uint8
	Gas() uint64
	GasPrice() *big.Int
	BlobGas() uint64
	Size() uint64
	MarshalBinary() ([]byte, error)
}

var _ Transaction = &DirectCall{}

type DirectCall struct {
	*types.DirectCall
}

func (dc DirectCall) Hash() (common.Hash, error) {
	return dc.DirectCall.Hash()
}

func (dc DirectCall) RawSignatureValues() (
	v *big.Int,
	r *big.Int,
	s *big.Int,
) {
	return big.NewInt(0), big.NewInt(0), big.NewInt(0)
}

func (dc DirectCall) From() (common.Address, error) {
	return dc.DirectCall.From.ToCommon(), nil
}

func (dc DirectCall) To() *common.Address {
	var to *common.Address
	if !dc.DirectCall.EmptyToField() {
		ct := dc.DirectCall.To.ToCommon()
		to = &ct
	}
	return to
}

func (dc DirectCall) Data() []byte {
	return dc.DirectCall.Data
}

func (dc DirectCall) Nonce() uint64 {
	return dc.DirectCall.Nonce
}

func (dc DirectCall) Value() *big.Int {
	return dc.DirectCall.Value
}

func (dc DirectCall) Type() uint8 {
	return dc.DirectCall.Type
}

func (dc DirectCall) Gas() uint64 {
	return dc.DirectCall.GasLimit
}

func (dc DirectCall) GasPrice() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) BlobGas() uint64 {
	return 0
}

func (dc DirectCall) Size() uint64 {
	encoded, err := dc.MarshalBinary()
	if err != nil {
		return 0
	}
	return uint64(len(encoded))
}

func (dc DirectCall) MarshalBinary() ([]byte, error) {
	return dc.DirectCall.Encode()
}

var _ Transaction = &TransactionCall{}

type TransactionCall struct {
	*gethTypes.Transaction
}

func (tc TransactionCall) Hash() (common.Hash, error) {
	return tc.Transaction.Hash(), nil
}

func (tc TransactionCall) From() (common.Address, error) {
	return gethTypes.Sender(
		gethTypes.LatestSignerForChainID(tc.ChainId()),
		tc.Transaction,
	)
}

func (tc TransactionCall) MarshalBinary() ([]byte, error) {
	encoded, err := tc.Transaction.MarshalBinary()
	return append([]byte{tc.Type()}, encoded...), err
}

// decodeTransaction takes a cadence event for transaction executed
// and decodes it into a Transaction interface. The concrete type
// will be either a TransactionCall or a DirectCall.
func decodeTransaction(event cadence.Event) (Transaction, error) {
	tx, err := types.DecodeTransactionEventPayload(event)
	if err != nil {
		return nil, fmt.Errorf("failed to cadence decode transaction: %w", err)
	}

	encodedTx, err := hex.DecodeString(tx.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction hex: %w", err)
	}

	// check if the transaction data is actually from a direct call,
	// which is a special state transition in Flow EVM.
	if tx.TransactionType == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(encodedTx)
		if err != nil {
			return nil, fmt.Errorf("failed to rlp decode direct call: %w", err)
		}

		return DirectCall{DirectCall: directCall}, nil
	}

	gethTx := &gethTypes.Transaction{}
	if err := gethTx.UnmarshalBinary(encodedTx); err != nil {
		return nil, fmt.Errorf("failed to rlp decode transaction: %w", err)
	}

	return TransactionCall{Transaction: gethTx}, nil
}

func UnmarshalTransaction(value []byte) (Transaction, error) {
	if value[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(value)
		if err != nil {
			return nil, fmt.Errorf("failed to rlp decode direct call: %w", err)
		}

		return DirectCall{DirectCall: directCall}, nil
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(value[1:]); err != nil {
		return nil, fmt.Errorf("failed to rlp decode transaction: %w", err)
	}

	return TransactionCall{Transaction: tx}, nil
}
