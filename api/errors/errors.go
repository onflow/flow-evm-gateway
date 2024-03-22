package errors

import (
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrNotSupported = errors.New("endpoint is not supported")
	ErrInvalid      = errors.New("invalid request")
	ErrInternal     = errors.New("internal error")
)

type ErrGasPriceTooLow struct {
	GasPrice *big.Int
}

func (e *ErrGasPriceTooLow) Error() string {
	return fmt.Sprintf(
		"the minimum accepted gas price for transactions is: %d",
		e.GasPrice,
	)
}

func NewErrGasPriceTooLow(gasPrice *big.Int) *ErrGasPriceTooLow {
	return &ErrGasPriceTooLow{
		GasPrice: gasPrice,
	}
}
