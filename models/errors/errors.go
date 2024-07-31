package errors

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/accounts/abi"
	"github.com/onflow/go-ethereum/common/hexutil"
	gethVM "github.com/onflow/go-ethereum/core/vm"
)

var (
	// API specific errors

	ErrNotSupported = errors.New("endpoint is not supported")
	ErrRateLimit    = errors.New("limit of requests per second reached")

	// General errors

	ErrInternal     = errors.New("internal error")
	ErrInvalid      = errors.New("invalid")
	ErrRecoverable  = errors.New("recoverable")
	ErrDisconnected = Recoverable(errors.New("disconnected"))

	// Transaction errors

	ErrFailedTransaction  = errors.New("failed transaction")
	ErrInvalidTransaction = errors.Join(ErrFailedTransaction, errors.New("invalid"))

	// Storage errors

	// ErrNotInitialized indicates storage instance was not correctly initialized and contains empty required values.
	ErrNotInitialized = errors.New("storage not initialized")
	// ErrNotFound indicates the resource does not exist.
	ErrNotFound = errors.New("entity not found")
	// ErrDuplicate indicates that the entity can not be stored due to an already existing same entity.
	ErrDuplicate = errors.New("entity duplicate")
	// ErrInvalidRange indicates that the block range provided as start and end height is invalid.
	ErrInvalidRange = errors.Join(ErrInvalid, errors.New("invalid block height range"))
	// ErrOutOfRange indicates the requested height is out of available range
	ErrOutOfRange = errors.Join(ErrInvalid, errors.New("height is out of available range"))
)

func FailedTransaction(reason string) error {
	return errors.Join(ErrFailedTransaction, errors.New(reason))
}

func InvalidTransaction(err error) error {
	return errors.Join(ErrInvalidTransaction, err)
}

func TransactionGasPriceTooLow(gasPrice *big.Int) error {
	return InvalidTransaction(fmt.Errorf(
		"the minimum accepted gas price for transactions is: %d",
		gasPrice,
	))
}

func Recoverable(err error) error {
	return errors.Join(ErrRecoverable, err)
}

// RevertError is an API error that encompasses an EVM revert with JSON error
// code and a binary data blob.
// We need this custom error type defined because the Geth server implementation
// expects this type when serialising the error API response.
type RevertError struct {
	error
	Reason string // revert reason hex encoded
}

// ErrorCode returns the JSON error code for a revert.
// See: https://github.com/ethereum/wiki/wiki/JSON-RPC-Error-Codes-Improvement-Proposal
func (e *RevertError) ErrorCode() int {
	return 3
}

// ErrorData returns the hex encoded revert reason.
func (e *RevertError) ErrorData() interface{} {
	return e.Reason
}

// NewRevertError creates a revertError instance with the provided revert data.
func NewRevertError(revert []byte) *RevertError {
	err := gethVM.ErrExecutionReverted

	reason, errUnpack := abi.UnpackRevert(revert)
	if errUnpack == nil {
		err = fmt.Errorf("%w: %v", gethVM.ErrExecutionReverted, reason)
	}
	return &RevertError{
		error:  err,
		Reason: hexutil.Encode(revert),
	}
}
