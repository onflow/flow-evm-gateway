package errors

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethVM "github.com/ethereum/go-ethereum/core/vm"
)

var (
	// API specific errors

	ErrEndpointNotSupported = errors.New("endpoint is not supported")
	ErrRateLimit            = errors.New("limit of requests per second reached")
	ErrIndexOnlyMode        = errors.New("transaction submission not allowed in index-only mode")
	ErrExceedLogQueryLimit  = errors.New("exceed max addresses or topics per search position")

	// General errors

	ErrInvalid             = errors.New("invalid")
	ErrRecoverable         = errors.New("recoverable")
	ErrDisconnected        = NewRecoverableError(errors.New("disconnected"))
	ErrMissingBlock        = errors.New("missing block")
	ErrMissingTransactions = errors.New("missing transactions")

	// Transaction errors

	ErrFailedTransaction  = errors.New("failed transaction")
	ErrInvalidTransaction = fmt.Errorf("%w: %w", ErrInvalid, ErrFailedTransaction)

	// Storage errors

	// ErrStorageNotInitialized indicates storage instance was not correctly initialized and contains empty required values.
	ErrStorageNotInitialized = errors.New("storage not initialized")
	// ErrEntityNotFound indicates the resource does not exist.
	ErrEntityNotFound = errors.New("entity not found")
	// ErrInvalidBlockRange indicates that the block range provided as start and end height is invalid.
	ErrInvalidBlockRange = fmt.Errorf("%w %w", ErrInvalid, errors.New("block height range"))
	// ErrHeightOutOfRange indicates the requested height is out of available range
	ErrHeightOutOfRange = fmt.Errorf("%w %w", ErrInvalid, errors.New("height not in available range"))
)

func NewEndpointNotSupportedError(endpoint string) error {
	return fmt.Errorf("%w: %s", ErrEndpointNotSupported, endpoint)
}

func NewHeightOutOfRangeError(height uint64) error {
	return fmt.Errorf("%w: %d", ErrHeightOutOfRange, height)
}

func NewFailedTransactionError(reason string) error {
	return fmt.Errorf("%w: %w", ErrFailedTransaction, errors.New(reason))
}

func NewInvalidTransactionError(err error) error {
	return fmt.Errorf("%w: %w", ErrInvalidTransaction, err)
}

func NewTxGasPriceTooLowError(gasPrice *big.Int) error {
	return NewInvalidTransactionError(fmt.Errorf(
		"the minimum accepted gas price for transactions is: %d",
		gasPrice,
	))
}

func NewRecoverableError(err error) error {
	return fmt.Errorf("%w: %w", ErrRecoverable, err)
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
func (e *RevertError) ErrorData() any {
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
