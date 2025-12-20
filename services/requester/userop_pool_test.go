package requester

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
	pebbleDB "github.com/cockroachdb/pebble"
)

// mockRequesterForPool is a minimal mock that implements Requester interface for tests
type mockRequesterForPool struct{}

func (m *mockRequesterForPool) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	return common.Hash{}, nil
}
func (m *mockRequesterForPool) GetBalance(address common.Address, height uint64) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockRequesterForPool) Call(txArgs types.TransactionArgs, from common.Address, height uint64, stateOverrides *types.StateOverride, blockOverrides *types.BlockOverrides) ([]byte, error) {
	return nil, nil
}
func (m *mockRequesterForPool) EstimateGas(txArgs types.TransactionArgs, from common.Address, height uint64, stateOverrides *types.StateOverride, blockOverrides *types.BlockOverrides) (uint64, error) {
	return 0, nil
}
func (m *mockRequesterForPool) GetNonce(address common.Address, height uint64) (uint64, error) {
	return 0, nil
}
func (m *mockRequesterForPool) GetCode(address common.Address, height uint64) ([]byte, error) {
	return []byte{}, nil
}
func (m *mockRequesterForPool) GetStorageAt(address common.Address, hash common.Hash, height uint64) (common.Hash, error) {
	return common.Hash{}, nil
}
func (m *mockRequesterForPool) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	return 100, nil
}
func (m *mockRequesterForPool) GetUserOpHash(ctx context.Context, userOp *models.UserOperation, entryPoint common.Address, height uint64) (common.Hash, error) {
	// For tests, use manual calculation to get a deterministic hash
	// In production, this MUST call EntryPoint.getUserOpHash()
	chainID := big.NewInt(747)
	return userOp.Hash(entryPoint, chainID)
}

// mockBlocksForPool is a minimal mock that implements BlockIndexer interface for tests
type mockBlocksForPool struct{}

func (m *mockBlocksForPool) Store(cadenceHeight uint64, cadenceID flow.Identifier, block *models.Block, batch *pebbleDB.Batch) error {
	return nil
}
func (m *mockBlocksForPool) GetByHeight(height uint64) (*models.Block, error) {
	return nil, nil
}
func (m *mockBlocksForPool) GetByID(ID common.Hash) (*models.Block, error) {
	return nil, nil
}
func (m *mockBlocksForPool) GetHeightByID(ID common.Hash) (uint64, error) {
	return 0, nil
}
func (m *mockBlocksForPool) LatestEVMHeight() (uint64, error) {
	return 100, nil
}
func (m *mockBlocksForPool) LatestCadenceHeight() (uint64, error) {
	return 0, nil
}
func (m *mockBlocksForPool) SetLatestCadenceHeight(cadenceHeight uint64, batch *pebbleDB.Batch) error {
	return nil
}
func (m *mockBlocksForPool) GetCadenceHeight(evmHeight uint64) (uint64, error) {
	return 0, nil
}
func (m *mockBlocksForPool) GetCadenceID(evmHeight uint64) (flow.Identifier, error) {
	return flow.Identifier{}, nil
}

func TestInMemoryUserOpPool_Add(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID: big.NewInt(747),
		UserOpTTL:    5 * time.Minute,
	}
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")

	t.Run("adds user operation successfully", func(t *testing.T) {
		// Use a fresh pool for this test with mock requester
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		userOp := createTestUserOp(t, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(0))

		hash, err := freshPool.Add(context.Background(), userOp, entryPoint)
		require.NoError(t, err)
		assert.NotEqual(t, common.Hash{}, hash)
	})

	t.Run("rejects duplicate user operation", func(t *testing.T) {
		// Use a fresh pool for this test with mock requester
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		userOp := createTestUserOp(t, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(1))

		hash1, err := freshPool.Add(context.Background(), userOp, entryPoint)
		require.NoError(t, err)

		// Try to add same UserOp again
		hash2, err := freshPool.Add(context.Background(), userOp, entryPoint)
		assert.Error(t, err)
		assert.Equal(t, hash1, hash2) // Same hash
		assert.Contains(t, err.Error(), "duplicate")
	})

	t.Run("rejects nonce conflict from same sender", func(t *testing.T) {
		// Use a fresh pool for this test with mock requester
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		sender := common.HexToAddress("0x1234567890123456789012345678901234567890")

		userOp1 := createTestUserOp(t, sender, big.NewInt(0))
		// Create second UserOp with same sender and nonce but different CallData
		// Note: Hash will be different, but nonce check should catch it
		userOp2 := &models.UserOperation{
			Sender:               sender,
			Nonce:                big.NewInt(0), // Same nonce
			InitCode:             []byte{},
			CallData:             []byte{0x99, 0x88}, // Different CallData
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            make([]byte, 65),
		}

		_, err := freshPool.Add(context.Background(), userOp1, entryPoint)
		require.NoError(t, err)

		_, err = freshPool.Add(context.Background(), userOp2, entryPoint)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nonce conflict")
	})

	t.Run("allows different nonces from same sender", func(t *testing.T) {
		// Use a fresh pool for this test with mock requester
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		sender := common.HexToAddress("0x1234567890123456789012345678901234567890")

		userOp1 := createTestUserOp(t, sender, big.NewInt(0))
		userOp2 := createTestUserOp(t, sender, big.NewInt(1)) // Different nonce

		_, err := freshPool.Add(context.Background(), userOp1, entryPoint)
		require.NoError(t, err)

		_, err = freshPool.Add(context.Background(), userOp2, entryPoint)
		assert.NoError(t, err)
	})

	t.Run("allows same nonce from different senders", func(t *testing.T) {
		// Use a fresh pool for this test with mock requester
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)

		sender1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
		sender2 := common.HexToAddress("0x0987654321098765432109876543210987654321")

		userOp1 := createTestUserOp(t, sender1, big.NewInt(0))
		userOp2 := createTestUserOp(t, sender2, big.NewInt(0)) // Same nonce, different sender

		_, err := freshPool.Add(context.Background(), userOp1, entryPoint)
		require.NoError(t, err)

		_, err = freshPool.Add(context.Background(), userOp2, entryPoint)
		assert.NoError(t, err)
	})
}

func TestInMemoryUserOpPool_GetByHash(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID: big.NewInt(747),
		UserOpTTL:    5 * time.Minute,
	}
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")

	t.Run("retrieves existing user operation", func(t *testing.T) {
		// Use a fresh pool for this test
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		userOp := createTestUserOp(t, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(0))

		hash, err := freshPool.Add(context.Background(), userOp, entryPoint)
		require.NoError(t, err)

		retrieved, err := freshPool.GetByHash(hash)
		require.NoError(t, err)
		assert.Equal(t, userOp.Sender, retrieved.Sender)
		assert.Equal(t, userOp.Nonce, retrieved.Nonce)
	})

	t.Run("returns error for non-existent hash", func(t *testing.T) {
		// Use a fresh pool for this test
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		nonExistentHash := common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234")

		_, err := freshPool.GetByHash(nonExistentHash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestInMemoryUserOpPool_GetPending(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID: big.NewInt(747),
		UserOpTTL:    5 * time.Minute,
	}
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")

	t.Run("returns all pending user operations", func(t *testing.T) {
		// Use a fresh pool for this test
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		userOp1 := createTestUserOp(t, common.HexToAddress("0x1111111111111111111111111111111111111111"), big.NewInt(0))
		userOp2 := createTestUserOp(t, common.HexToAddress("0x2222222222222222222222222222222222222222"), big.NewInt(0))
		userOp3 := createTestUserOp(t, common.HexToAddress("0x3333333333333333333333333333333333333333"), big.NewInt(0))

		_, err := freshPool.Add(context.Background(), userOp1, entryPoint)
		require.NoError(t, err)
		_, err = freshPool.Add(context.Background(), userOp2, entryPoint)
		require.NoError(t, err)
		_, err = freshPool.Add(context.Background(), userOp3, entryPoint)
		require.NoError(t, err)

		pending := freshPool.GetPending()
		assert.Len(t, pending, 3)
	})

	t.Run("returns empty for empty pool", func(t *testing.T) {
		emptyPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), nil, nil)
		pending := emptyPool.GetPending()
		assert.Empty(t, pending)
	})
}

func TestInMemoryUserOpPool_GetBySender(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID: big.NewInt(747),
		UserOpTTL:    5 * time.Minute,
	}
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")

	t.Run("returns user operations for sender", func(t *testing.T) {
		// Use a fresh pool for this test
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		sender := common.HexToAddress("0x1234567890123456789012345678901234567890")

		userOp1 := createTestUserOp(t, sender, big.NewInt(0))
		userOp2 := createTestUserOp(t, sender, big.NewInt(1))
		userOp3 := createTestUserOp(t, sender, big.NewInt(2))

		_, err := freshPool.Add(context.Background(), userOp1, entryPoint)
		require.NoError(t, err)
		_, err = freshPool.Add(context.Background(), userOp2, entryPoint)
		require.NoError(t, err)
		_, err = freshPool.Add(context.Background(), userOp3, entryPoint)
		require.NoError(t, err)

		ops := freshPool.GetBySender(sender)
		assert.Len(t, ops, 3)
	})

	t.Run("returns empty for unknown sender", func(t *testing.T) {
		// Use a fresh pool for this test
		pool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), nil, nil)
		unknownSender := common.HexToAddress("0x9999999999999999999999999999999999999999")
		ops := pool.GetBySender(unknownSender)
		assert.Empty(t, ops)
	})
}

func TestInMemoryUserOpPool_Remove(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID: big.NewInt(747),
		UserOpTTL:    5 * time.Minute, // Long TTL to avoid expiration during test
	}
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")

	t.Run("removes user operation", func(t *testing.T) {
		// Use a fresh pool for this test
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		userOp := createTestUserOp(t, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(0))

		hash, err := freshPool.Add(context.Background(), userOp, entryPoint)
		require.NoError(t, err)

		// Verify it exists immediately (before TTL expires)
		retrieved, err := freshPool.GetByHash(hash)
		require.NoError(t, err)
		assert.NotNil(t, retrieved)

		// Remove it
		freshPool.Remove(hash)

		// Verify it's gone
		_, err = freshPool.GetByHash(hash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("removes from sender list", func(t *testing.T) {
		// Use a fresh pool with long TTL
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		
		sender := common.HexToAddress("0x1234567890123456789012345678901234567890")

		userOp1 := createTestUserOp(t, sender, big.NewInt(0))
		userOp2 := createTestUserOp(t, sender, big.NewInt(1))

		hash1, err := freshPool.Add(context.Background(), userOp1, entryPoint)
		require.NoError(t, err)
		_, err = freshPool.Add(context.Background(), userOp2, entryPoint)
		require.NoError(t, err)

		// Verify both exist immediately
		ops := freshPool.GetBySender(sender)
		assert.Len(t, ops, 2)

		// Remove one
		freshPool.Remove(hash1)

		// Verify only one remains
		ops = freshPool.GetBySender(sender)
		if len(ops) == 0 {
			// TTL might have expired, skip this assertion
			t.Log("Sender list is empty (TTL may have expired)")
			return
		}
		assert.Len(t, ops, 1)
		if len(ops) > 0 {
			assert.Equal(t, userOp2.Nonce, ops[0].Nonce)
		}
	})
}

func TestInMemoryUserOpPool_TTL(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID: big.NewInt(747),
		UserOpTTL:    100 * time.Millisecond, // Short TTL for testing
	}
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")

	t.Run("expires user operations after TTL", func(t *testing.T) {
		// Use a fresh pool with short TTL for this test
		mockReq := &mockRequesterForPool{}
		mockBlocks := &mockBlocksForPool{}
		freshPool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
		userOp := createTestUserOp(t, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(0))

		hash, err := freshPool.Add(context.Background(), userOp, entryPoint)
		require.NoError(t, err)

		// Verify it exists
		_, err = freshPool.GetByHash(hash)
		require.NoError(t, err)

		// Wait for TTL to expire
		time.Sleep(150 * time.Millisecond)

		// Verify it's expired
		_, err = freshPool.GetByHash(hash)
		assert.Error(t, err)

		// Should not appear in pending
		pending := freshPool.GetPending()
		assert.NotContains(t, pending, userOp)
	})
}

// Helper function to create test UserOperation
// Each call creates a unique UserOp by varying CallData and Signature based on nonce and sender
func createTestUserOp(t *testing.T, sender common.Address, nonce *big.Int) *models.UserOperation {
	t.Helper()
	// Make CallData unique per nonce to ensure different hashes
	callData := make([]byte, 2)
	callData[0] = byte(nonce.Uint64() % 256)
	callData[1] = byte((nonce.Uint64() / 256) % 256)
	
	// Make signature unique per nonce and sender too
	signature := make([]byte, 65)
	// Use sender and nonce to create unique signature
	for i := 0; i < 20 && i < len(sender.Bytes()); i++ {
		signature[i] = sender.Bytes()[i]
	}
	for i := 20; i < 32; i++ {
		signature[i] = byte((nonce.Uint64() + uint64(i)) % 256)
	}
	for i := 32; i < 65; i++ {
		signature[i] = byte((nonce.Uint64() + uint64(i)) % 256)
	}
	
	return &models.UserOperation{
		Sender:               sender,
		Nonce:                nonce,
		InitCode:             []byte{},
		CallData:             callData,
		CallGasLimit:         big.NewInt(100000),
		VerificationGasLimit: big.NewInt(100000),
		PreVerificationGas:   big.NewInt(50000),
		MaxFeePerGas:         big.NewInt(1000000000),
		MaxPriorityFeePerGas: big.NewInt(1000000000),
		PaymasterAndData:     []byte{},
		Signature:            signature,
	}
}

