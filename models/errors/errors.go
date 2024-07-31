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

	ErrInternal = errors.New("internal error")
	ErrInvalid  = errors.New("invalid")

	// Transaction errors

	ErrFailedTransaction  = errors.New("failed transaction")
	ErrInvalidTransaction = errors.Join(ErrFailedTransaction, errors.New("invalid"))
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

// RevertError is an API error that encompasses an EVM revert with JSON error
// code and a binary data blob.
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
