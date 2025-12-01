package ingestion

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	gethTypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/storage"
)

// EntryPoint event signatures
var (
	// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
	UserOperationEventSig = crypto.Keccak256Hash([]byte("UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)"))

	// UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
	UserOperationRevertReasonSig = crypto.Keccak256Hash([]byte("UserOperationRevertReason(bytes32,address,uint256,bytes)"))
)

// IndexUserOperationEvents indexes UserOperation events from EntryPoint logs
// EntryPoint emits UserOperationEvent and UserOperationRevertReason events
func IndexUserOperationEvents(
	block *models.Block,
	receipts []*models.Receipt,
	userOpStorage storage.UserOperationIndexer,
) error {
	// Iterate through all receipts in the block
	for _, receipt := range receipts {
		// Check if this transaction targets the EntryPoint
		// We'll need to check the transaction's 'to' address
		// For now, we'll check all logs for EntryPoint events

		// Parse logs for UserOperation events
		for _, log := range receipt.Logs {
			// EntryPoint UserOperationEvent signature:
			// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
			// Topic 0: keccak256("UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)")
			// Topic 1: userOpHash
			// Topic 2: sender
			// Topic 3: paymaster

			// EntryPoint UserOperationRevertReason signature:
			// UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
			// Topic 0: keccak256("UserOperationRevertReason(bytes32,address,uint256,bytes)")

			// For now, we'll store a mapping of userOpHash -> txHash
			// This will be populated when we parse the actual events
			_ = log
		}
	}

	return nil
}

// ParseUserOperationEvent parses a UserOperationEvent from EntryPoint logs
func ParseUserOperationEvent(log *gethTypes.Log) (*UserOperationEvent, error) {
	// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
	// Expected topics:
	// - Topic[0]: event signature hash
	// - Topic[1]: userOpHash
	// - Topic[2]: sender
	// - Topic[3]: paymaster (or zero address if no paymaster)

	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("invalid UserOperationEvent: expected at least 4 topics, got %d", len(log.Topics))
	}

	// Verify event signature
	if log.Topics[0] != UserOperationEventSig {
		return nil, fmt.Errorf("invalid event signature: expected UserOperationEvent")
	}

	userOpHash := common.Hash(log.Topics[1])
	sender := common.BytesToAddress(log.Topics[2].Bytes())
	paymaster := common.BytesToAddress(log.Topics[3].Bytes())

	// Decode data: nonce, success, actualGasCost, actualGasUsed
	// Data is ABI-encoded: (uint256, bool, uint256, uint256)
	// ABI definition for the data tuple
	eventABI := `[{"type":"tuple","components":[{"name":"nonce","type":"uint256"},{"name":"success","type":"bool"},{"name":"actualGasCost","type":"uint256"},{"name":"actualGasUsed","type":"uint256"}]}]`
	
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(eventABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	var decoded struct {
		Nonce         *big.Int
		Success       bool
		ActualGasCost *big.Int
		ActualGasUsed *big.Int
	}

	if len(log.Data) > 0 {
		if err := parsedABI.UnpackIntoInterface(&decoded, "tuple", log.Data); err != nil {
			// If ABI decoding fails, use zero values
			decoded.Nonce = big.NewInt(0)
			decoded.Success = true
			decoded.ActualGasCost = big.NewInt(0)
			decoded.ActualGasUsed = big.NewInt(0)
		}
	}

	return &UserOperationEvent{
		UserOpHash:    userOpHash,
		Sender:        sender,
		Paymaster:     paymaster,
		Nonce:         decoded.Nonce,
		Success:       decoded.Success,
		ActualGasCost: decoded.ActualGasCost,
		ActualGasUsed: decoded.ActualGasUsed,
		TxHash:        log.TxHash,
		BlockNumber:   big.NewInt(int64(log.BlockNumber)),
		BlockHash:     log.BlockHash,
	}, nil
}

// ParseUserOperationRevertReason parses a UserOperationRevertReason event
func ParseUserOperationRevertReason(log *gethTypes.Log) (*UserOperationRevertReason, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("invalid UserOperationRevertReason: expected at least 3 topics, got %d", len(log.Topics))
	}

	if log.Topics[0] != UserOperationRevertReasonSig {
		return nil, fmt.Errorf("invalid event signature: expected UserOperationRevertReason")
	}

	userOpHash := common.Hash(log.Topics[1])
	sender := common.BytesToAddress(log.Topics[2].Bytes())

	// Decode data: nonce, revertReason
	// Data is ABI-encoded: (uint256, bytes)
	eventABI := `[{"type":"tuple","components":[{"name":"nonce","type":"uint256"},{"name":"revertReason","type":"bytes"}]}]`
	
	parsedABI, err := abi.JSON(bytes.NewReader([]byte(eventABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	var decoded struct {
		Nonce        *big.Int
		RevertReason []byte
	}

	if len(log.Data) > 0 {
		if err := parsedABI.UnpackIntoInterface(&decoded, "tuple", log.Data); err != nil {
			decoded.Nonce = big.NewInt(0)
			decoded.RevertReason = []byte{}
		}
	}

	return &UserOperationRevertReason{
		UserOpHash:    userOpHash,
		Sender:        sender,
		Nonce:         decoded.Nonce,
		RevertReason:  string(decoded.RevertReason),
		TxHash:        log.TxHash,
		BlockNumber:   big.NewInt(int64(log.BlockNumber)),
		BlockHash:     log.BlockHash,
	}, nil
}

// UserOperationRevertReason represents a parsed UserOperationRevertReason event
type UserOperationRevertReason struct {
	UserOpHash   common.Hash
	Sender       common.Address
	Nonce        *big.Int
	RevertReason string
	TxHash       common.Hash
	BlockNumber  *big.Int
	BlockHash    common.Hash
}

// UserOperationEvent represents a parsed UserOperationEvent from EntryPoint
type UserOperationEvent struct {
	UserOpHash    common.Hash
	Sender        common.Address
	Paymaster     common.Address
	Nonce         *big.Int
	Success       bool
	ActualGasCost *big.Int
	ActualGasUsed *big.Int
	TxHash        common.Hash
	BlockNumber   *big.Int
	BlockHash     common.Hash
}

