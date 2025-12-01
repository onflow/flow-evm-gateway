package models

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserOperation_Hash(t *testing.T) {
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")
	chainID := big.NewInt(747)

	t.Run("computes correct hash", func(t *testing.T) {
		userOp := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{0x01, 0x02},
		}

		hash, err := userOp.Hash(entryPoint, chainID)
		require.NoError(t, err)
		assert.NotEqual(t, common.Hash{}, hash)
		assert.Equal(t, 32, len(hash.Bytes()))
	})

	t.Run("same userOp produces same hash", func(t *testing.T) {
		userOp := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(1),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{0x01, 0x02},
		}

		hash1, err := userOp.Hash(entryPoint, chainID)
		require.NoError(t, err)

		hash2, err := userOp.Hash(entryPoint, chainID)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("different nonce produces different hash", func(t *testing.T) {
		userOp1 := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{0x01, 0x02},
		}

		userOp2 := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(1),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{0x01, 0x02},
		}

		hash1, err := userOp1.Hash(entryPoint, chainID)
		require.NoError(t, err)

		hash2, err := userOp2.Hash(entryPoint, chainID)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("different entryPoint produces different hash", func(t *testing.T) {
		userOp := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{0x01, 0x02},
		}

		entryPoint1 := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")
		entryPoint2 := common.HexToAddress("0x0000000000000000000000000000000000000001")

		hash1, err := userOp.Hash(entryPoint1, chainID)
		require.NoError(t, err)

		hash2, err := userOp.Hash(entryPoint2, chainID)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})
}

func TestUserOperation_VerifySignature(t *testing.T) {
	entryPoint := common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789")
	chainID := big.NewInt(747)

	t.Run("verifies valid signature", func(t *testing.T) {
		// Create a private key for signing
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		userOp := &UserOperation{
			Sender:               crypto.PubkeyToAddress(privateKey.PublicKey),
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{},
		}

		// Get UserOp hash (EntryPoint v0.9.0 format)
		hash, err := userOp.Hash(entryPoint, chainID)
		require.NoError(t, err)

		// ERC-4337 uses EIP-191 style signing: keccak256("\x19\x01" || chainId || userOpHash)
		sigHash := crypto.Keccak256Hash(
			[]byte("\x19\x01"),
			chainID.Bytes(),
			hash.Bytes(),
		)

		// Sign the EIP-191 hash
		signature, err := crypto.Sign(sigHash.Bytes(), privateKey)
		require.NoError(t, err)

		// Add signature to UserOp
		userOp.Signature = signature

		// Verify
		valid, err := userOp.VerifySignature(entryPoint, chainID)
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("rejects invalid signature", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		userOp := &UserOperation{
			Sender:               crypto.PubkeyToAddress(privateKey.PublicKey),
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{0x01, 0x02, 0x03}, // Invalid signature (too short)
		}

		valid, err := userOp.VerifySignature(entryPoint, chainID)
		// Should return error for invalid signature length
		assert.Error(t, err)
		assert.False(t, valid)
	})

	t.Run("rejects signature from different sender", func(t *testing.T) {
		privateKey1, err := crypto.GenerateKey()
		require.NoError(t, err)

		privateKey2, err := crypto.GenerateKey()
		require.NoError(t, err)

		userOp := &UserOperation{
			Sender:               crypto.PubkeyToAddress(privateKey1.PublicKey), // Sender is key1
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{0x12, 0x34},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{},
		}

		// Get UserOp hash (EntryPoint v0.9.0 format)
		hash, err := userOp.Hash(entryPoint, chainID)
		require.NoError(t, err)

		signature, err := crypto.Sign(hash.Bytes(), privateKey2) // Sign with wrong key
		require.NoError(t, err)

		userOp.Signature = signature

		// Verification should fail
		valid, err := userOp.VerifySignature(entryPoint, chainID)
		require.NoError(t, err)
		assert.False(t, valid)
	})
}

func TestUserOperation_PackForSignature(t *testing.T) {
	t.Run("packs correctly", func(t *testing.T) {
		userOp := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(42),
			InitCode:             []byte{0xaa, 0xbb},
			CallData:             []byte{0xcc, 0xdd},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(200000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(500000000),
			PaymasterAndData:     []byte{0xee, 0xff},
			Signature:            []byte{},
		}

		packed, err := userOp.PackForSignature()
		require.NoError(t, err)
		assert.NotEmpty(t, packed)

		// Verify structure: should include only UserOp fields (no entryPoint or chainID)
		// Total length should be: 20 (sender) + 32*9 (other fields) = 308 bytes
		assert.Equal(t, 308, len(packed))
	})

	t.Run("different UserOps produce different packed data", func(t *testing.T) {
		userOp1 := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(0),
			InitCode:             []byte{},
			CallData:             []byte{},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{},
		}

		userOp2 := &UserOperation{
			Sender:               common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Nonce:                big.NewInt(1), // Different nonce
			InitCode:             []byte{},
			CallData:             []byte{},
			CallGasLimit:         big.NewInt(100000),
			VerificationGasLimit: big.NewInt(100000),
			PreVerificationGas:   big.NewInt(50000),
			MaxFeePerGas:         big.NewInt(1000000000),
			MaxPriorityFeePerGas: big.NewInt(1000000000),
			PaymasterAndData:     []byte{},
			Signature:            []byte{},
		}

		packed1, err := userOp1.PackForSignature()
		require.NoError(t, err)

		packed2, err := userOp2.PackForSignature()
		require.NoError(t, err)

		assert.NotEqual(t, packed1, packed2)
	})
}

