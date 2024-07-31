package models

import (
	"errors"
	"fmt"
)

var (
	ErrDisconnected          = NewRecoverableError(errors.New("disconnected"))
	ErrInvalidEVMTransaction = errors.New("invalid evm transaction")
)

func NewRecoverableError(err error) RecoverableError {
	return RecoverableError{err}
}

// RecoverableError is used to signal any types of errors that if encountered
// could be retried again
type RecoverableError struct {
	err error
}

func (r RecoverableError) Unwrap() error {
	return r.err
}

func (r RecoverableError) Error() string {
	return fmt.Sprintf("recoverable error: %v", r.err)
}

func IsRecoverableError(err error) bool {
	return errors.As(err, &RecoverableError{})
}

func NewInvalidEVMTransaction(err error) error {
	return errors.Join(ErrInvalidEVMTransaction, err)
}
