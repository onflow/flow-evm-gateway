package models

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/types"
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

type txEventPayload struct {
	BlockHeight             uint64 `cadence:"blockHeight"`
	BlockHash               string `cadence:"blockHash"`
	TransactionHash         string `cadence:"transactionHash"`
	Transaction             string `cadence:"transaction"`
	Failed                  bool   `cadence:"failed"`
	VMError                 string `cadence:"vmError"`
	TransactionType         uint8  `cadence:"transactionType"`
	GasConsumed             uint64 `cadence:"gasConsumed"`
	DeployedContractAddress string `cadence:"deployedContractAddress"`
	ReturnedValue           string `cadence:"returnedValue"`
	Logs                    string `cadence:"logs"`
}

func (tx *txEventPayload) IsDirectCall() bool {
	return tx.TransactionType == types.DirectCallTxType
}

// DecodeTransaction takes a cadence event for transaction executed
// and decodes it into a Transaction interface. The concrete type
// will be either a TransactionCall or a DirectCall.
func DecodeTransaction(event cadence.Event) (
	Transaction,
	error,
) {
	if !IsTransactionExecutedEvent(event) {
		return nil, fmt.Errorf(
			"invalid event type for decoding into receipt, received %s expected %s",
			event.Type().ID(),
			types.EventTypeTransactionExecuted,
		)
	}

	t := &txEventPayload{}
	err := cadence.DecodeFields(event, t)
	if err != nil {
		return nil, fmt.Errorf("failed to cadence decode transaction: %w", err)
	}

	encodedTx, err := hex.DecodeString(t.Transaction)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction hex: %w", err)
	}

	// check if the transaction data is actually from a direct call,
	// which is a special state transition in Flow EVM.
	if t.IsDirectCall() {
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
