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
	"github.com/onflow/go-ethereum/core/txpool"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/rlp"
)

const (
	// txSlotSize is used to calculate how many data slots a single transaction
	// takes up based on its size. The slots are used as DoS protection, ensuring
	// that validating a new transaction remains a constant operation (in reality
	// O(maxslots), where max slots are 4 currently).
	TxSlotSize = 32 * 1024

	// txMaxSize is the maximum size a single transaction can have. This field has
	// non-trivial consequences: larger transactions are significantly harder and
	// more expensive to propagate; larger transactions also take more resources
	// to validate whether they fit into the pool or not.
	TxMaxSize = 4 * TxSlotSize // 128KB
)

type Transaction interface {
	Hash() common.Hash
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
	// TEMP: Remove `blockHeight` after PreviewNet is reset
	blockHeight uint64
}

// TEMP: Remove `DirectCallHashCalculationBlockHeightChange` after PreviewNet is reset
var DirectCallHashCalculationBlockHeightChange uint64 = 0

func (dc DirectCall) Hash() common.Hash {
	// Use the NEW hash calculation
	if dc.blockHeight >= DirectCallHashCalculationBlockHeightChange {
		return dc.DirectCall.Hash()
	}

	// Use the OLD hash calculation
	tx := gethTypes.NewTx(&gethTypes.LegacyTx{
		GasPrice: big.NewInt(0),
		Gas:      dc.GasLimit,
		To:       dc.To(),
		Value:    dc.Value(),
		Data:     dc.Data(),
		Nonce:    dc.Nonce(),
	})
	return tx.Hash()
}

func (dc DirectCall) RawSignatureValues() (
	v *big.Int,
	r *big.Int,
	s *big.Int,
) {
	return dc.Transaction().RawSignatureValues()
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

func (tc TransactionCall) Hash() common.Hash {
	return tc.Transaction.Hash()
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

// decodeTransactionEvent takes a cadence event for transaction executed
// and decodes its payload into a Transaction interface and a StorageReceipt.
// The concrete type will be either a TransactionCall or a DirectCall.
func decodeTransactionEvent(
	event cadence.Event,
) (Transaction, *StorageReceipt, error) {
	txEvent, err := types.DecodeTransactionEventPayload(event)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to Cadence decode transaction event: %w", err)
	}

	encodedTx, err := hex.DecodeString(txEvent.Payload)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to hex-decode transaction payload: %w", err)
	}

	encodedLogs, err := hex.DecodeString(txEvent.Logs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to hex decode receipt: %w", err)
	}

	var logs []*gethTypes.Log
	if len(encodedLogs) > 0 {
		err = rlp.Decode(bytes.NewReader(encodedLogs), &logs)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to RLP-decode receipt: %w", err)
		}
	}

	gethReceipt := &gethTypes.Receipt{
		BlockNumber:       big.NewInt(int64(txEvent.BlockHeight)),
		Type:              txEvent.TransactionType,
		Logs:              logs,
		TxHash:            common.HexToHash(txEvent.Hash),
		ContractAddress:   common.HexToAddress(txEvent.ContractAddress),
		GasUsed:           txEvent.GasConsumed,
		CumulativeGasUsed: txEvent.GasConsumed, // todo use cumulative after added to the tx result
		TransactionIndex:  uint(txEvent.Index),
		BlockHash:         common.HexToHash(txEvent.BlockHash),
	}

	if txEvent.ErrorCode == uint16(types.ErrCodeNoError) {
		gethReceipt.Status = gethTypes.ReceiptStatusSuccessful
	} else {
		gethReceipt.Status = gethTypes.ReceiptStatusFailed
	}

	gethReceipt.Bloom = gethTypes.CreateBloom([]*gethTypes.Receipt{gethReceipt})

	receipt := NewStorageReceipt(gethReceipt)
	if txEvent.ErrorCode == uint16(types.ExecutionErrCodeExecutionReverted) {
		revert, err := hex.DecodeString(txEvent.ReturnedData)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to hex-decode transaction return data: %w", err)
		}
		receipt.RevertReason = revert
	}

	var tx Transaction
	// check if the transaction payload is actually from a direct call,
	// which is a special state transition in Flow EVM.
	if txEvent.TransactionType == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(encodedTx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to RLP-decode direct call: %w", err)
		}
		evmHeight := receipt.BlockNumber.Uint64()

		tx = DirectCall{DirectCall: directCall, blockHeight: evmHeight}
	} else {
		gethTx := &gethTypes.Transaction{}
		if err := gethTx.UnmarshalBinary(encodedTx); err != nil {
			return nil, nil, fmt.Errorf("failed to RLP-decode transaction: %w", err)
		}
		tx = TransactionCall{Transaction: gethTx}
	}

	// since there's no base fee we can always use gas price
	receipt.EffectiveGasPrice = tx.GasPrice()

	return tx, receipt, nil
}

func UnmarshalTransaction(value []byte, blockHeight uint64) (Transaction, error) {
	if value[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(value)
		if err != nil {
			return nil, fmt.Errorf("failed to RLP-decode direct call: %w", err)
		}

		// TEMP: Remove `blockHeight` after PreviewNet is reset
		return DirectCall{DirectCall: directCall, blockHeight: blockHeight}, nil
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(value[1:]); err != nil {
		// todo remove this after previewnet is reset
		// breaking change on transaction data, try without type
		if err := tx.UnmarshalBinary(value); err == nil {
			return TransactionCall{Transaction: tx}, nil
		}

		return nil, fmt.Errorf("failed to RLP-decode transaction: %w", err)
	}

	return TransactionCall{Transaction: tx}, nil
}

func ValidateTransaction(
	tx *gethTypes.Transaction,
	head *gethTypes.Header,
	signer gethTypes.Signer,
	opts *txpool.ValidationOptions,
) error {
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

	if err := txpool.ValidateTransaction(tx, head, signer, opts); err != nil {
		return err
	}

	return nil
}
