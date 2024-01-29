package storage

import "errors"

var (
	// NotInitialized indicates storage instance was not correctly initialized and contains empty required values.
	NotInitialized = errors.New("storage not initialized")
	// NotFound indicates the resource does not exist.
	NotFound = errors.New("entity not found")
)
