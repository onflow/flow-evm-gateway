package errors

import "errors"

var (
	// ErrNotInitialized indicates storage instance was not correctly initialized and contains empty required values.
	ErrNotInitialized = errors.New("storage not initialized")
	// ErrNotFound indicates the resource does not exist.
	ErrNotFound = errors.New("entity not found")
	// ErrDuplicate indicates that the entity can not be stored due to an already existing same entity.
	ErrDuplicate = errors.New("entity duplicate")
	// ErrInvalidRange indicates that the block range provided as start and end height is invalid.
	ErrInvalidRange = errors.New("invalid block height range")
)
