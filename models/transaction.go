package models

import (
	"bytes"
	"encoding/hex"
	"errors"
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
	GasFeeCap() *big.Int
	GasTipCap() *big.Int
	GasPrice() *big.Int
	BlobGas() uint64
	BlobGasFeeCap() *big.Int
	BlobHashes() []common.Hash
	Size() uint64
	AccessList() gethTypes.AccessList
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
	return dc.DirectCall.Transaction().Type()
}

func (dc DirectCall) Gas() uint64 {
	return dc.DirectCall.GasLimit
}

func (dc DirectCall) GasFeeCap() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) GasTipCap() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) GasPrice() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) BlobGas() uint64 {
	return 0
}

func (dc DirectCall) BlobGasFeeCap() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) BlobHashes() []common.Hash {
	return []common.Hash{}
}

func (dc DirectCall) Size() uint64 {
	encoded, err := dc.MarshalBinary()
	if err != nil {
		return 0
	}
	return uint64(len(encoded))
}

func (dc DirectCall) AccessList() gethTypes.AccessList {
	return gethTypes.AccessList{}
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
		// todo remove this after previewnet is reset
		// breaking change on transaction data, try without type
		if err := tx.UnmarshalBinary(value); err == nil {
			return TransactionCall{Transaction: tx}, nil
		}

		return nil, fmt.Errorf("failed to rlp decode transaction: %w", err)
	}

	return TransactionCall{Transaction: tx}, nil
}

func ValidateTransaction(tx *gethTypes.Transaction) error {
	txDataLen := len(tx.Data())

	// Contract creation doesn't validate call data, handle first
	if tx.To() == nil {
		// Contract creation should contain sufficient data to deploy a contract. A
		// typical error is omitting sender due to some quirk in the javascript call
		// e.g. https://github.com/onflow/go-ethereum/issues/16106.
		if txDataLen == 0 {
			// Prevent sending ether into black hole (show stopper)
			if tx.Value().Cmp(big.NewInt(0)) > 0 {
				return errors.New("transaction will create a contract with value but empty code")
			}
			// No value submitted at least, critically Warn, but don't blow up
			return errors.New("transaction will create a contract with empty code")
		}

		if txDataLen < 40 { // arbitrary heuristic limit
			return fmt.Errorf(
				"transaction will create a contract, but the payload is suspiciously small (%d bytes)",
				txDataLen,
			)
		}
	}

	// Not a contract creation, validate as a plain transaction
	if tx.To() != nil {
		to := common.NewMixedcaseAddress(*tx.To())
		if !to.ValidChecksum() {
			return errors.New("invalid checksum on recipient address")
		}

		if bytes.Equal(tx.To().Bytes(), common.Address{}.Bytes()) {
			return errors.New("transaction recipient is the zero address")
		}

		// If the data is not empty, validate that it has the 4byte prefix and the rest divisible by 32 bytes
		if txDataLen > 0 {
			if txDataLen < 4 {
				return errors.New("transaction data is not valid ABI (missing the 4 byte call prefix)")
			}

			if n := txDataLen - 4; n%32 != 0 {
				return fmt.Errorf(
					"transaction data is not valid ABI (length should be a multiple of 32 (was %d))",
					n,
				)
			}
		}
	}

	return nil
}
