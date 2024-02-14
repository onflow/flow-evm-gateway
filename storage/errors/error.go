package errors

import "errors"

var (
	// NotInitialized indicates storage instance was not correctly initialized and contains empty required values.
	NotInitialized = errors.New("storage not initialized")
	// NotFound indicates the resource does not exist.
	NotFound = errors.New("entity not found")
	// Duplicate indicates that the entity can not be stored due to an already existing same entity.
	Duplicate = errors.New("entity duplicate")
	// InvalidRange indicates that the block range provided as start and end height is invalid.
	InvalidRange = errors.New("invalid block height range")
)
