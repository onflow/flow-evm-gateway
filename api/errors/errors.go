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
	ErrNotSupported = errors.New("endpoint is not supported")
	ErrInvalid      = errors.New("invalid request")
	ErrInternal     = errors.New("internal error")
	ErrRateLimit    = errors.New("limit of requests per second reached")
)

type GasPriceTooLowError struct {
	GasPrice *big.Int
}

func (e *GasPriceTooLowError) Error() string {
	return fmt.Sprintf(
		"the minimum accepted gas price for transactions is: %d",
		e.GasPrice,
	)
}

func NewErrGasPriceTooLow(gasPrice *big.Int) *GasPriceTooLowError {
	return &GasPriceTooLowError{
		GasPrice: gasPrice,
	}
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
