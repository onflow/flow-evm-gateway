package errors

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/accounts/abi"
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

	ErrFailedTransaction   = errors.New("failed transaction")
	ErrInvalidTransaction  = errors.Join(ErrFailedTransaction, errors.New("invalid"))
	ErrRevertedTransaction = errors.Join(ErrFailedTransaction, errors.New("reverted"))
)

func FailedTransaction(err error) error {
	return errors.Join(ErrFailedTransaction, err)
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

func RevertedTransaction(reason []byte) error {
	err := gethVM.ErrExecutionReverted

	r, errUnpack := abi.UnpackRevert(reason)
	if errUnpack == nil {
		err = fmt.Errorf("%w: %v", gethVM.ErrExecutionReverted, r)
	}

	return errors.Join(ErrRevertedTransaction, err)
}
