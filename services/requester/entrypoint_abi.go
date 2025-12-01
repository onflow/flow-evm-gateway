package requester

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
)

// EntryPointABI is the ABI for the ERC-4337 EntryPoint contract
// This is a simplified version - in production, load from JSON
const entryPointABI = `[
	{
		"inputs": [
			{
				"components": [
					{"internalType": "address", "name": "sender", "type": "address"},
					{"internalType": "uint256", "name": "nonce", "type": "uint256"},
					{"internalType": "bytes", "name": "initCode", "type": "bytes"},
					{"internalType": "bytes", "name": "callData", "type": "bytes"},
					{"internalType": "uint256", "name": "callGasLimit", "type": "uint256"},
					{"internalType": "uint256", "name": "verificationGasLimit", "type": "uint256"},
					{"internalType": "uint256", "name": "preVerificationGas", "type": "uint256"},
					{"internalType": "uint256", "name": "maxFeePerGas", "type": "uint256"},
					{"internalType": "uint256", "name": "maxPriorityFeePerGas", "type": "uint256"},
					{"internalType": "bytes", "name": "paymasterAndData", "type": "bytes"},
					{"internalType": "bytes", "name": "signature", "type": "bytes"}
				],
				"internalType": "struct UserOperation[]",
				"name": "ops",
				"type": "tuple[]"
			},
			{
				"internalType": "address payable",
				"name": "beneficiary",
				"type": "address"
			}
		],
		"name": "handleOps",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{
				"components": [
					{"internalType": "address", "name": "sender", "type": "address"},
					{"internalType": "uint256", "name": "nonce", "type": "uint256"},
					{"internalType": "bytes", "name": "initCode", "type": "bytes"},
					{"internalType": "bytes", "name": "callData", "type": "bytes"},
					{"internalType": "uint256", "name": "callGasLimit", "type": "uint256"},
					{"internalType": "uint256", "name": "verificationGasLimit", "type": "uint256"},
					{"internalType": "uint256", "name": "preVerificationGas", "type": "uint256"},
					{"internalType": "uint256", "name": "maxFeePerGas", "type": "uint256"},
					{"internalType": "uint256", "name": "maxPriorityFeePerGas", "type": "uint256"},
					{"internalType": "bytes", "name": "paymasterAndData", "type": "bytes"},
					{"internalType": "bytes", "name": "signature", "type": "bytes"}
				],
				"internalType": "struct UserOperation",
				"name": "userOp",
				"type": "tuple"
			}
		],
		"name": "simulateValidation",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{
				"internalType": "address",
				"name": "account",
				"type": "address"
			}
		],
		"name": "getDeposit",
		"outputs": [
			{
				"internalType": "uint256",
				"name": "",
				"type": "uint256"
			}
		],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "senderCreator",
		"outputs": [
			{
				"internalType": "address",
				"name": "",
				"type": "address"
			}
		],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{
				"internalType": "uint256",
				"name": "opIndex",
				"type": "uint256"
			},
			{
				"internalType": "string",
				"name": "reason",
				"type": "string"
			}
		],
		"name": "FailedOp",
		"type": "error"
	},
	{
		"inputs": [
			{
				"internalType": "uint256",
				"name": "opIndex",
				"type": "uint256"
			},
			{
				"internalType": "string",
				"name": "reason",
				"type": "string"
			},
			{
				"internalType": "bytes",
				"name": "revertData",
				"type": "bytes"
			}
		],
		"name": "FailedOpWithRevert",
		"type": "error"
	}
]`

var entryPointABIParsed abi.ABI

func init() {
	var err error
	entryPointABIParsed, err = abi.JSON(bytes.NewReader([]byte(entryPointABI)))
	if err != nil {
		panic(fmt.Sprintf("failed to parse EntryPoint ABI: %v", err))
	}
}

// UserOperationABI is the struct format expected by the ABI encoder
type UserOperationABI struct {
	Sender               common.Address
	Nonce                *big.Int
	InitCode             []byte
	CallData             []byte
	CallGasLimit         *big.Int
	VerificationGasLimit *big.Int
	PreVerificationGas   *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	PaymasterAndData     []byte
	Signature            []byte
}

// EncodeHandleOps encodes the calldata for EntryPoint.handleOps()
func EncodeHandleOps(userOps []*models.UserOperation, beneficiary common.Address) ([]byte, error) {
	// Convert UserOperations to ABI format using concrete struct type
	ops := make([]UserOperationABI, len(userOps))
	for i, userOp := range userOps {
		ops[i] = UserOperationABI{
			Sender:               userOp.Sender,
			Nonce:                userOp.Nonce,
			InitCode:             userOp.InitCode,
			CallData:             userOp.CallData,
			CallGasLimit:         userOp.CallGasLimit,
			VerificationGasLimit:  userOp.VerificationGasLimit,
			PreVerificationGas:    userOp.PreVerificationGas,
			MaxFeePerGas:          userOp.MaxFeePerGas,
			MaxPriorityFeePerGas:  userOp.MaxPriorityFeePerGas,
			PaymasterAndData:      userOp.PaymasterAndData,
			Signature:             userOp.Signature,
		}
	}

	// Encode the function call
	data, err := entryPointABIParsed.Pack("handleOps", ops, beneficiary)
	if err != nil {
		return nil, fmt.Errorf("failed to encode handleOps: %w", err)
	}

	return data, nil
}

// EncodeSimulateValidation encodes the calldata for EntryPoint.simulateValidation()
func EncodeSimulateValidation(userOp *models.UserOperation) ([]byte, error) {
	op := struct {
		Sender               common.Address
		Nonce                *big.Int
		InitCode             []byte
		CallData             []byte
		CallGasLimit         *big.Int
		VerificationGasLimit *big.Int
		PreVerificationGas   *big.Int
		MaxFeePerGas         *big.Int
		MaxPriorityFeePerGas *big.Int
		PaymasterAndData     []byte
		Signature            []byte
	}{
		Sender:               userOp.Sender,
		Nonce:                userOp.Nonce,
		InitCode:             userOp.InitCode,
		CallData:             userOp.CallData,
		CallGasLimit:         userOp.CallGasLimit,
		VerificationGasLimit:  userOp.VerificationGasLimit,
		PreVerificationGas:    userOp.PreVerificationGas,
		MaxFeePerGas:          userOp.MaxFeePerGas,
		MaxPriorityFeePerGas:  userOp.MaxPriorityFeePerGas,
		PaymasterAndData:      userOp.PaymasterAndData,
		Signature:             userOp.Signature,
	}

	// Encode the function call
	data, err := entryPointABIParsed.Pack("simulateValidation", op)
	if err != nil {
		return nil, fmt.Errorf("failed to encode simulateValidation: %w", err)
	}

	return data, nil
}

// EncodeGetDeposit encodes the calldata for EntryPoint.getDeposit()
func EncodeGetDeposit(account common.Address) ([]byte, error) {
	data, err := entryPointABIParsed.Pack("getDeposit", account)
	if err != nil {
		return nil, fmt.Errorf("failed to encode getDeposit: %w", err)
	}
	return data, nil
}

// EncodeSenderCreator encodes the calldata for EntryPoint.senderCreator()
func EncodeSenderCreator() ([]byte, error) {
	data, err := entryPointABIParsed.Pack("senderCreator")
	if err != nil {
		return nil, fmt.Errorf("failed to encode senderCreator: %w", err)
	}
	return data, nil
}

// GetHandleOpsSelector returns the 4-byte function selector for handleOps
func GetHandleOpsSelector() []byte {
	// keccak256("handleOps((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes),address)")[:4]
	// This is the standard selector
	selector, _ := hex.DecodeString("b61d27f6")
	return selector
}

