package pebble

import (
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/onflow/flow-evm-gateway/storage"
)

var _ storage.UserOperationIndexer = &UserOperations{}

type UserOperations struct {
	store *Storage
}

func NewUserOperations(store *Storage) *UserOperations {
	return &UserOperations{
		store: store,
	}
}

// StoreUserOpReceipt stores a UserOperation receipt
func (u *UserOperations) StoreUserOpReceipt(
	userOpHash common.Hash,
	receipt *storage.UserOperationReceipt,
	batch *pebble.Batch,
) error {
	key := userOpHash.Bytes()
	value, err := rlp.EncodeToBytes(receipt)
	if err != nil {
		return fmt.Errorf("failed to encode user operation receipt: %w", err)
	}

	return u.store.set(userOpReceiptKey, key, value, batch)
}

// GetUserOpReceipt retrieves a UserOperation receipt by hash
func (u *UserOperations) GetUserOpReceipt(userOpHash common.Hash) (*storage.UserOperationReceipt, error) {
	key := userOpHash.Bytes()
	value, err := u.store.get(userOpReceiptKey, key)
	if err != nil {
		return nil, err
	}

	var receipt storage.UserOperationReceipt
	if err := rlp.DecodeBytes(value, &receipt); err != nil {
		return nil, fmt.Errorf("failed to decode user operation receipt: %w", err)
	}

	return &receipt, nil
}

// StoreUserOpTxMapping stores the mapping from userOpHash to transaction hash
func (u *UserOperations) StoreUserOpTxMapping(
	userOpHash common.Hash,
	txHash common.Hash,
	batch *pebble.Batch,
) error {
	key := userOpHash.Bytes()
	value := txHash.Bytes()
	return u.store.set(userOpTxMappingKey, key, value, batch)
}

// GetTxHashByUserOpHash retrieves the transaction hash for a UserOperation
func (u *UserOperations) GetTxHashByUserOpHash(userOpHash common.Hash) (common.Hash, error) {
	key := userOpHash.Bytes()
	value, err := u.store.get(userOpTxMappingKey, key)
	if err != nil {
		return common.Hash{}, err
	}

	return common.BytesToHash(value), nil
}

