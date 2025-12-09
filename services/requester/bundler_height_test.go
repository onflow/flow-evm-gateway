package requester

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go-sdk"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/config"
	ethTypes "github.com/onflow/flow-evm-gateway/eth/types"
	"github.com/onflow/flow-evm-gateway/models"
	pebbleDB "github.com/cockroachdb/pebble"
)

// mockBlockIndexerForHeightTest is a mock that tracks which method was called
type mockBlockIndexerForHeightTest struct {
	latestEVMHeightCalled bool
	latestEVMHeightValue  uint64
	latestEVMHeightError  error
}

func (m *mockBlockIndexerForHeightTest) Store(cadenceHeight uint64, cadenceID flow.Identifier, block *models.Block, batch *pebbleDB.Batch) error {
	return nil
}

func (m *mockBlockIndexerForHeightTest) GetByHeight(height uint64) (*models.Block, error) {
	return nil, nil
}

func (m *mockBlockIndexerForHeightTest) GetByID(ID common.Hash) (*models.Block, error) {
	return nil, nil
}

func (m *mockBlockIndexerForHeightTest) GetHeightByID(ID common.Hash) (uint64, error) {
	return 0, nil
}

func (m *mockBlockIndexerForHeightTest) LatestEVMHeight() (uint64, error) {
	m.latestEVMHeightCalled = true
	return m.latestEVMHeightValue, m.latestEVMHeightError
}

func (m *mockBlockIndexerForHeightTest) LatestCadenceHeight() (uint64, error) {
	return 0, nil
}

func (m *mockBlockIndexerForHeightTest) SetLatestCadenceHeight(cadenceHeight uint64, batch *pebbleDB.Batch) error {
	return nil
}

func (m *mockBlockIndexerForHeightTest) GetCadenceHeight(height uint64) (uint64, error) {
	return 0, nil
}

func (m *mockBlockIndexerForHeightTest) GetCadenceID(height uint64) (flow.Identifier, error) {
	return flow.Identifier{}, nil
}

// mockRequesterForHeightTest tracks if GetLatestEVMHeight was called (it shouldn't be)
type mockRequesterForHeightTest struct {
	getLatestEVMHeightCalled bool
	getUserOpHashCalled      bool
}

func (m *mockRequesterForHeightTest) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	return common.Hash{}, nil
}

func (m *mockRequesterForHeightTest) GetBalance(address common.Address, height uint64) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockRequesterForHeightTest) Call(txArgs ethTypes.TransactionArgs, from common.Address, height uint64, stateOverrides *ethTypes.StateOverride, blockOverrides *ethTypes.BlockOverrides) ([]byte, error) {
	return []byte{}, nil
}

func (m *mockRequesterForHeightTest) EstimateGas(txArgs ethTypes.TransactionArgs, from common.Address, height uint64, stateOverrides *ethTypes.StateOverride, blockOverrides *ethTypes.BlockOverrides) (uint64, error) {
	return 200000, nil
}

func (m *mockRequesterForHeightTest) GetNonce(address common.Address, height uint64) (uint64, error) {
	return 0, nil
}

func (m *mockRequesterForHeightTest) GetCode(address common.Address, height uint64) ([]byte, error) {
	return []byte{}, nil
}

func (m *mockRequesterForHeightTest) GetStorageAt(address common.Address, hash common.Hash, height uint64) (common.Hash, error) {
	return common.Hash{}, nil
}

func (m *mockRequesterForHeightTest) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	m.getLatestEVMHeightCalled = true
	return 0, errors.New("GetLatestEVMHeight should not be called - bundler should use blocks.LatestEVMHeight()")
}

func (m *mockRequesterForHeightTest) GetUserOpHash(ctx context.Context, userOp *models.UserOperation, entryPoint common.Address, height uint64) (common.Hash, error) {
	m.getUserOpHashCalled = true
	// Return a dummy hash
	return common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"), nil
}

// TestBundler_UsesIndexedHeight verifies that the bundler uses blocks.LatestEVMHeight()
// instead of requester.GetLatestEVMHeight() when calling GetUserOpHash
func TestBundler_UsesIndexedHeight(t *testing.T) {
	cfg := config.Config{
		EVMNetworkID:      big.NewInt(545),
		BundlerEnabled:    true,
		MaxOpsPerBundle:   10,
		EntryPointAddress: common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"),
		Coinbase:          common.HexToAddress("0x3cC530e139Dd93641c3F30217B20163EF8b17159"),
	}

	mockReq := &mockRequesterForHeightTest{}
	mockBlocks := &mockBlockIndexerForHeightTest{
		latestEVMHeightValue: 100,
		latestEVMHeightError:  nil,
	}
	pool := NewInMemoryUserOpPool(cfg, zerolog.Nop(), mockReq, mockBlocks)
	txPool := &mockTxPool{}

	bundler := NewBundler(pool, cfg, zerolog.Nop(), txPool, mockReq, mockBlocks)

	// Add a UserOp to the pool
	userOp := createTestUserOpForBundler(t, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(0))
	_, err := pool.Add(context.Background(), userOp, cfg.EntryPointAddress)
	require.NoError(t, err)

	// Try to create bundled transactions - this should call blocks.LatestEVMHeight(), not requester.GetLatestEVMHeight()
	_, err = bundler.CreateBundledTransactions(context.Background())

	// Verify that blocks.LatestEVMHeight() was called
	assert.True(t, mockBlocks.latestEVMHeightCalled, "bundler should call blocks.LatestEVMHeight()")

	// Verify that requester.GetLatestEVMHeight() was NOT called
	assert.False(t, mockReq.getLatestEVMHeightCalled, "bundler should NOT call requester.GetLatestEVMHeight() - it should use indexed height instead")

	// Verify that GetUserOpHash was called (which means it used the indexed height)
	assert.True(t, mockReq.getUserOpHashCalled, "bundler should call GetUserOpHash with the indexed height")
}

