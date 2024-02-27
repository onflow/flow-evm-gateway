package errors

import "errors"

var (
	ErrNotSupported = errors.New("endpoint is not supported")
)
