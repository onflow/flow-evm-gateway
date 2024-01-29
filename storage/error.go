package storage

import "errors"

var (
	NotInitialized = errors.New("storage not initialized")
	NotFound       = errors.New("entity not found")
)
