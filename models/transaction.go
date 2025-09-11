package models

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/txpool"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
)

const (
	// TxSlotSize is used to calculate how many data slots a single transaction
	// takes up based on its size. The slots are used as DoS protection, ensuring
	// that validating a new transaction remains a constant operation (in reality
	// O(maxslots), where max slots are 4 currently).
	TxSlotSize = 32 * 1024

	// TxMaxSize is the maximum size a single transaction can have. This field has
	// non-trivial consequences: larger transactions are significantly harder and
	// more expensive to propagate; larger transactions also take more resources
	// to validate whether they fit into the pool or not.
	TxMaxSize = 4 * TxSlotSize // 128KB
)

var BaseFeePerGas = big.NewInt(1)

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
	ChainId() *big.Int
	BlobGas() uint64
	BlobGasFeeCap() *big.Int
	BlobHashes() []common.Hash
	Size() uint64
	AccessList() gethTypes.AccessList
	SetCodeAuthorizations() []gethTypes.SetCodeAuthorization
	MarshalBinary() ([]byte, error)
}

var _ Transaction = &DirectCall{}

type DirectCall struct {
	*types.DirectCall
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
	// for contract deployments, `to` should always be `nil`
	if dc.SubType == types.DeployCallSubType {
		return nil
	}

	var to *common.Address
	if !dc.EmptyToField() {
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
	return dc.GasLimit
}

func (dc DirectCall) GasFeeCap() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) GasTipCap() *big.Int {
	return big.NewInt(0)
}

func (dc DirectCall) GasPrice() *big.Int {
	return BaseFeePerGas
}

func (dc DirectCall) ChainId() *big.Int {
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
	return rlp.ListSize(uint64(len(encoded)))
}

func (dc DirectCall) AccessList() gethTypes.AccessList {
	return gethTypes.AccessList{}
}

func (dc DirectCall) SetCodeAuthorizations() []gethTypes.SetCodeAuthorization {
	return []gethTypes.SetCodeAuthorization{}
}

func (dc DirectCall) MarshalBinary() ([]byte, error) {
	return dc.Encode()
}

var _ Transaction = &TransactionCall{}

type TransactionCall struct {
	*gethTypes.Transaction
}

func (tc TransactionCall) GasPrice() *big.Int {
	// EIP-1559 introduced a new fee model in Ethereum that replaces the
	// legacy `GasPrice` with `MaxFeePerGas` and `MaxPriorityFeePerGas`.
	// However, many Ethereum tools and wallets (such as MetaMask, Hardhat, etc.)
	// still rely on reading `GasPrice`, even if it’s not explicitly set.
	//
	// When a user submits an EIP-1559 transaction type, `GasPrice` is not
	// specified, Ethereum nodes typically return the current `BaseFeePerGas`
	// as the effective `GasPrice` for compatibility reasons.
	//
	// This behavior is mirrored here in Flow EVM Gateway: if `GasPrice` is zero,
	// we return the configured `BaseFeePerGas` to satisfy the expectations of
	// these tools.
	//
	// This does NOT affect Flow’s actual transaction fee calculation,
	// this is purely for compatibility.
	if tc.Transaction.GasPrice().Sign() == 0 {
		return BaseFeePerGas
	}
	return tc.Transaction.GasPrice()
}

func (tc TransactionCall) GasFeeCap() *big.Int {
	// `GasFeeCap` represents the `MaxFeePerGas` in EIP-1559, the maximum fee
	// a user is willing to pay. Ethereum clients expect a non-zero value when
	// calculating effective gas prices for compatibility.
	//
	// If the user does not provide a value (zero), this method returns the
	// configured `BaseFeePerGas` to avoid confusion and comply with expected
	// EVM behavior. This ensures Ethereum tooling can still function correctly
	// when interacting with Flow EVM.
	if tc.Transaction.GasFeeCap().Sign() == 0 {
		return BaseFeePerGas
	}
	return tc.Transaction.GasFeeCap()
}

func (tc TransactionCall) GasTipCap() *big.Int {
	// `GasTipCap` represents the `MaxPriorityFeePerGas` in EIP-1559, the optional
	// "tip" to incentivize block inclusion. Ethereum expects this value to be
	// explicitly defined or it defaults to something reasonable like the base fee.
	//
	// To satisfy Ethereum clients and maintain expected behavior, when this value
	// is zero, Flow EVM returns the configured `BaseFeePerGas` as a safe default.
	// This ensures Ethereum tools can continue to compute `EffectiveGasPrice`
	// without errors.
	if tc.Transaction.GasTipCap().Sign() == 0 {
		return BaseFeePerGas
	}
	return tc.Transaction.GasTipCap()
}

func (tc TransactionCall) Hash() common.Hash {
	return tc.Transaction.Hash()
}

func (tc TransactionCall) From() (common.Address, error) {
	return DeriveTxSender(tc.Transaction)
}

func (tc TransactionCall) MarshalBinary() ([]byte, error) {
	encoded, err := tc.Transaction.MarshalBinary()
	return append([]byte{tc.Type()}, encoded...), err
}

// decodeTransactionEvent takes a cadence event for transaction executed
// and decodes its payload into a Transaction interface and a Receipt.
// The concrete type will be either a TransactionCall or a DirectCall.
func decodeTransactionEvent(event cadence.Event) (
	Transaction,
	*Receipt,
	*events.TransactionEventPayload,
	error,
) {
	txEvent, err := events.DecodeTransactionEventPayload(event)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"failed to Cadence decode transaction event [%s]: %w",
			event.String(),
			err,
		)
	}

	gethReceipt := &gethTypes.Receipt{
		BlockNumber:       big.NewInt(int64(txEvent.BlockHeight)),
		Type:              txEvent.TransactionType,
		TxHash:            txEvent.Hash,
		ContractAddress:   common.HexToAddress(txEvent.ContractAddress),
		GasUsed:           txEvent.GasConsumed,
		TransactionIndex:  uint(txEvent.Index),
		EffectiveGasPrice: big.NewInt(0),
	}

	if len(txEvent.Logs) > 0 {
		err = rlp.Decode(bytes.NewReader(txEvent.Logs), &gethReceipt.Logs)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to RLP-decode logs: %w", err)
		}
	}

	if txEvent.ErrorCode == uint16(types.ErrCodeNoError) {
		gethReceipt.Status = gethTypes.ReceiptStatusSuccessful
	} else {
		gethReceipt.Status = gethTypes.ReceiptStatusFailed
	}

	gethReceipt.Bloom = gethTypes.CreateBloom(gethReceipt)

	var revertReason []byte
	if txEvent.ErrorCode == uint16(types.ExecutionErrCodeExecutionReverted) {
		revertReason = txEvent.ReturnedData
	}

	receipt := NewReceipt(gethReceipt, revertReason, txEvent.PrecompiledCalls)

	var tx Transaction
	// check if the transaction payload is actually from a direct call,
	// which is a special state transition in Flow EVM.
	if txEvent.TransactionType == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(txEvent.Payload)
		if err != nil {
			return nil, nil, nil, fmt.Errorf(
				"failed to RLP-decode direct call [%x]: %w",
				txEvent.Payload,
				err,
			)
		}
		receipt.EffectiveGasPrice = BaseFeePerGas
		tx = DirectCall{DirectCall: directCall}
	} else {
		gethTx := &gethTypes.Transaction{}
		if err := gethTx.UnmarshalBinary(txEvent.Payload); err != nil {
			return nil, nil, nil, fmt.Errorf(
				"failed to RLP-decode transaction [%x]: %w",
				txEvent.Payload,
				err,
			)
		}
		if gethTx.GasPrice().Sign() == 0 {
			receipt.EffectiveGasPrice = BaseFeePerGas
		} else {
			receipt.EffectiveGasPrice, err = gethTx.EffectiveGasTip(nil)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		tx = TransactionCall{Transaction: gethTx}
	}

	return tx, receipt, txEvent, nil
}

func UnmarshalTransaction(value []byte) (Transaction, error) {
	if value[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(value)
		if err != nil {
			return nil, fmt.Errorf("failed to RLP-decode direct call [%x]: %w", value, err)
		}

		return DirectCall{DirectCall: directCall}, nil
	}

	tx := &gethTypes.Transaction{}
	if err := tx.UnmarshalBinary(value[1:]); err != nil {
		return nil, fmt.Errorf("failed to RLP-decode transaction [%x]: %w", value, err)
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
		// e.g. https://github.com/ethereum/go-ethereum/issues/16106.
		if txDataLen == 0 {
			// Prevent sending ether into black hole (show stopper)
			if tx.Value().Cmp(big.NewInt(0)) > 0 {
				return errors.New("transaction will create a contract with value but empty code")
			}
			// No value submitted at least, critically Warn, but don't blow up
			return errors.New("transaction will create a contract with empty code")
		}
	}

	// Not a contract creation, validate as a plain transaction
	if tx.To() != nil {
		if bytes.Equal(tx.To().Bytes(), common.Address{}.Bytes()) {
			return errors.New("transaction recipient is the zero address")
		}
	}

	if err := txpool.ValidateTransaction(tx, head, signer, opts); err != nil {
		return err
	}

	return nil
}

// DeriveTxSender returns the address derived from the signature (V, R, S)
// using secp256k1 elliptic curve and an error if it failed deriving or
// upon an incorrect signature.
func DeriveTxSender(tx *gethTypes.Transaction) (common.Address, error) {
	var signer gethTypes.Signer
	if chainID := tx.ChainId(); chainID.Sign() != 0 {
		signer = gethTypes.LatestSignerForChainID(chainID)
	} else {
		signer = gethTypes.HomesteadSigner{}
	}

	from, err := gethTypes.Sender(signer, tx)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to derive the sender: %w", err)
	}

	return from, nil
}
