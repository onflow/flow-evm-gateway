package requester

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/abis"
)

// Old hardcoded ABIs removed - now loading from services/abis/*.json files
// EntryPointABI is the ABI for the ERC-4337 EntryPoint contract (v0.7+)
// Uses PackedUserOperation format
// NOTE: EntryPoint.json doesn't include simulateValidation - use EntryPointSimulations ABI for that
const _deprecated_entryPointABI = `[
	{
		"inputs": [
			{
				"components": [
					{"internalType": "address", "name": "sender", "type": "address"},
					{"internalType": "uint256", "name": "nonce", "type": "uint256"},
					{"internalType": "bytes", "name": "initCode", "type": "bytes"},
					{"internalType": "bytes", "name": "callData", "type": "bytes"},
					{"internalType": "bytes32", "name": "accountGasLimits", "type": "bytes32"},
					{"internalType": "uint256", "name": "preVerificationGas", "type": "uint256"},
					{"internalType": "bytes32", "name": "gasFees", "type": "bytes32"},
					{"internalType": "bytes", "name": "paymasterAndData", "type": "bytes"},
					{"internalType": "bytes", "name": "signature", "type": "bytes"}
				],
				"internalType": "struct PackedUserOperation[]",
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
				"components": [
					{"internalType": "address", "name": "sender", "type": "address"},
					{"internalType": "uint256", "name": "nonce", "type": "uint256"},
					{"internalType": "bytes", "name": "initCode", "type": "bytes"},
					{"internalType": "bytes", "name": "callData", "type": "bytes"},
					{"internalType": "bytes32", "name": "accountGasLimits", "type": "bytes32"},
					{"internalType": "uint256", "name": "preVerificationGas", "type": "uint256"},
					{"internalType": "bytes32", "name": "gasFees", "type": "bytes32"},
					{"internalType": "bytes", "name": "paymasterAndData", "type": "bytes"},
					{"internalType": "bytes", "name": "signature", "type": "bytes"}
				],
				"internalType": "struct PackedUserOperation",
				"name": "userOp",
				"type": "tuple"
			}
		],
		"name": "getUserOpHash",
		"outputs": [
			{
				"internalType": "bytes32",
				"name": "",
				"type": "bytes32"
			}
		],
		"stateMutability": "view",
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
					{"internalType": "bytes32", "name": "accountGasLimits", "type": "bytes32"},
					{"internalType": "uint256", "name": "preVerificationGas", "type": "uint256"},
					{"internalType": "bytes32", "name": "gasFees", "type": "bytes32"},
					{"internalType": "bytes", "name": "paymasterAndData", "type": "bytes"},
					{"internalType": "bytes", "name": "signature", "type": "bytes"}
				],
				"internalType": "struct PackedUserOperation",
				"name": "userOp",
				"type": "tuple"
			}
		],
		"name": "simulateValidation",
		"outputs": [
			{
				"components": [
					{
						"components": [
							{"internalType": "uint256", "name": "preOpGas", "type": "uint256"},
							{"internalType": "uint256", "name": "prefund", "type": "uint256"},
							{"internalType": "uint256", "name": "accountValidationData", "type": "uint256"},
							{"internalType": "uint256", "name": "paymasterValidationData", "type": "uint256"},
							{"internalType": "bytes", "name": "paymasterContext", "type": "bytes"}
						],
						"internalType": "struct IEntryPoint.ReturnInfo",
						"name": "returnInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "uint256", "name": "stake", "type": "uint256"},
							{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
						],
						"internalType": "struct IStakeManager.StakeInfo",
						"name": "senderInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "uint256", "name": "stake", "type": "uint256"},
							{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
						],
						"internalType": "struct IStakeManager.StakeInfo",
						"name": "factoryInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "uint256", "name": "stake", "type": "uint256"},
							{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
						],
						"internalType": "struct IStakeManager.StakeInfo",
						"name": "paymasterInfo",
						"type": "tuple"
					}
				],
				"internalType": "struct IEntryPoint.ValidationResult",
				"name": "",
				"type": "tuple"
			}
		],
		"stateMutability": "nonpayable",
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
var entryPointSimulationsABIParsed abi.ABI
var simpleAccountFactoryABIParsed *abi.ABI

// EntryPointSimulationsABI is the ABI for EntryPointSimulations contract (v0.7+)
// Uses PackedUserOperation format instead of UserOperation
const entryPointSimulationsABI = `[
	{
		"inputs": [
			{
				"components": [
					{"internalType": "address", "name": "sender", "type": "address"},
					{"internalType": "uint256", "name": "nonce", "type": "uint256"},
					{"internalType": "bytes", "name": "initCode", "type": "bytes"},
					{"internalType": "bytes", "name": "callData", "type": "bytes"},
					{"internalType": "bytes32", "name": "accountGasLimits", "type": "bytes32"},
					{"internalType": "uint256", "name": "preVerificationGas", "type": "uint256"},
					{"internalType": "bytes32", "name": "gasFees", "type": "bytes32"},
					{"internalType": "bytes", "name": "paymasterAndData", "type": "bytes"},
					{"internalType": "bytes", "name": "signature", "type": "bytes"}
				],
				"internalType": "struct PackedUserOperation",
				"name": "userOp",
				"type": "tuple"
			}
		],
		"name": "simulateValidation",
		"outputs": [
			{
				"components": [
					{
						"components": [
							{"internalType": "uint256", "name": "preOpGas", "type": "uint256"},
							{"internalType": "uint256", "name": "prefund", "type": "uint256"},
							{"internalType": "uint256", "name": "accountValidationData", "type": "uint256"},
							{"internalType": "uint256", "name": "paymasterValidationData", "type": "uint256"},
							{"internalType": "bytes", "name": "paymasterContext", "type": "bytes"}
						],
						"internalType": "struct IEntryPoint.ReturnInfo",
						"name": "returnInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "uint256", "name": "stake", "type": "uint256"},
							{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
						],
						"internalType": "struct IStakeManager.StakeInfo",
						"name": "senderInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "uint256", "name": "stake", "type": "uint256"},
							{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
						],
						"internalType": "struct IStakeManager.StakeInfo",
						"name": "factoryInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "uint256", "name": "stake", "type": "uint256"},
							{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
						],
						"internalType": "struct IStakeManager.StakeInfo",
						"name": "paymasterInfo",
						"type": "tuple"
					},
					{
						"components": [
							{"internalType": "address", "name": "aggregator", "type": "address"},
							{
								"components": [
									{"internalType": "uint256", "name": "stake", "type": "uint256"},
									{"internalType": "uint256", "name": "unstakeDelaySec", "type": "uint256"}
								],
								"internalType": "struct IStakeManager.StakeInfo",
								"name": "stakeInfo",
								"type": "tuple"
							}
						],
						"internalType": "struct IEntryPointSimulations.AggregatorStakeInfo",
						"name": "aggregatorInfo",
						"type": "tuple"
					}
				],
				"internalType": "struct IEntryPointSimulations.ValidationResult",
				"name": "",
				"type": "tuple"
			}
		],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

// SimpleAccountFactoryABI is the ABI for SimpleAccountFactory contract
const simpleAccountFactoryABI = `[
	{
		"inputs": [
			{
				"internalType": "contract IEntryPoint",
				"name": "_entryPoint",
				"type": "address"
			}
		],
		"stateMutability": "nonpayable",
		"type": "constructor"
	},
	{
		"inputs": [
			{
				"internalType": "address",
				"name": "msgSender",
				"type": "address"
			},
			{
				"internalType": "address",
				"name": "entity",
				"type": "address"
			},
			{
				"internalType": "address",
				"name": "senderCreator",
				"type": "address"
			}
		],
		"name": "NotSenderCreator",
		"type": "error"
	},
	{
		"inputs": [],
		"name": "accountImplementation",
		"outputs": [
			{
				"internalType": "contract SimpleAccount",
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
				"internalType": "address",
				"name": "owner",
				"type": "address"
			},
			{
				"internalType": "uint256",
				"name": "salt",
				"type": "uint256"
			}
		],
		"name": "createAccount",
		"outputs": [
			{
				"internalType": "contract SimpleAccount",
				"name": "ret",
				"type": "address"
			}
		],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{
				"internalType": "address",
				"name": "owner",
				"type": "address"
			},
			{
				"internalType": "uint256",
				"name": "salt",
				"type": "uint256"
			}
		],
		"name": "getAddress",
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
		"inputs": [],
		"name": "senderCreator",
		"outputs": [
			{
				"internalType": "contract ISenderCreator",
				"name": "",
				"type": "address"
			}
		],
		"stateMutability": "view",
		"type": "function"
	}
]`

func init() {
	var err error
	
	// Load EntryPoint ABI from embedded JSON
	var entryPointArtifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(abis.EntryPointJSON, &entryPointArtifact); err != nil {
		panic(fmt.Sprintf("failed to unmarshal EntryPoint JSON: %v", err))
	}
	entryPointABIParsed, err = abi.JSON(bytes.NewReader(entryPointArtifact.ABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse EntryPoint ABI: %v", err))
	}
	
	// Load EntryPointSimulations ABI from embedded JSON
	var entryPointSimulationsArtifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(abis.EntryPointSimulationsJSON, &entryPointSimulationsArtifact); err != nil {
		panic(fmt.Sprintf("failed to unmarshal EntryPointSimulations JSON: %v", err))
	}
	entryPointSimulationsABIParsed, err = abi.JSON(bytes.NewReader(entryPointSimulationsArtifact.ABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse EntryPointSimulations ABI: %v", err))
	}
	
	// Load SimpleAccountFactory ABI from embedded JSON
	var simpleAccountFactoryArtifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(abis.SimpleAccountFactoryJSON, &simpleAccountFactoryArtifact); err != nil {
		panic(fmt.Sprintf("failed to unmarshal SimpleAccountFactory JSON: %v", err))
	}
	parsed, err := abi.JSON(bytes.NewReader(simpleAccountFactoryArtifact.ABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse SimpleAccountFactory ABI: %v", err))
	}
	simpleAccountFactoryABIParsed = &parsed
}

// UserOperationABI is the struct format for old EntryPoint v0.6 (deprecated - kept for backwards compatibility)
// Note: EntryPoint v0.7+ uses PackedUserOperation format
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
// EntryPoint v0.7+ uses PackedUserOperation format
func EncodeHandleOps(userOps []*models.UserOperation, beneficiary common.Address) ([]byte, error) {
	// Convert UserOperations to PackedUserOperation format
	ops := make([]PackedUserOperationABI, len(userOps))
	for i, userOp := range userOps {
		ops[i] = PackedUserOperationABI{
			Sender:             userOp.Sender,
			Nonce:              userOp.Nonce,
			InitCode:           userOp.InitCode,
			CallData:           userOp.CallData,
			AccountGasLimits:   packAccountGasLimits(userOp.CallGasLimit, userOp.VerificationGasLimit),
			PreVerificationGas: userOp.PreVerificationGas,
			GasFees:            packGasFees(userOp.MaxFeePerGas, userOp.MaxPriorityFeePerGas),
			PaymasterAndData:   userOp.PaymasterAndData,
			Signature:          userOp.Signature,
		}
	}

	// Encode the function call
	data, err := entryPointABIParsed.Pack("handleOps", ops, beneficiary)
	if err != nil {
		return nil, fmt.Errorf("failed to encode handleOps: %w", err)
	}

	return data, nil
}

// GetUserOpHash is a placeholder function - the actual call is made by requester.GetUserOpHash()
// This function exists only for documentation purposes and should not be called directly
func GetUserOpHash(userOp *models.UserOperation, entryPoint common.Address) (common.Hash, error) {
	// Note: This function is not used - the actual call is made by requester.GetUserOpHash()
	// This function exists only for documentation purposes
	return common.Hash{}, fmt.Errorf("getUserOpHash must be called via requester - use requester.GetUserOpHash()")
}

// PackedUserOperationABI is the struct format for EntryPointSimulations (v0.7+)
type PackedUserOperationABI struct {
	Sender             common.Address
	Nonce              *big.Int
	InitCode           []byte
	CallData           []byte
	AccountGasLimits   [32]byte // bytes32: packed callGasLimit (uint128) + verificationGasLimit (uint128)
	PreVerificationGas *big.Int
	GasFees            [32]byte // bytes32: packed maxFeePerGas (uint128) + maxPriorityFeePerGas (uint128)
	PaymasterAndData   []byte
	Signature          []byte
}

// packAccountGasLimits packs callGasLimit and verificationGasLimit into a bytes32
// Solidity format: bytes32(uint256(verificationGasLimit) << 128 | uint256(callGasLimit))
// bytes 0-15 (high 128 bits): verificationGasLimit
// bytes 16-31 (low 128 bits): callGasLimit
func packAccountGasLimits(callGasLimit, verificationGasLimit *big.Int) [32]byte {
	var result [32]byte

	// Pack callGasLimit into lower 16 bytes (bytes 16-31, right-aligned)
	if callGasLimit != nil {
		callGasBytes := make([]byte, 16)
		callGasLimit.FillBytes(callGasBytes)
		copy(result[16:32], callGasBytes)
	}

	// Pack verificationGasLimit into upper 16 bytes (bytes 0-15, right-aligned)
	if verificationGasLimit != nil {
		verificationGasBytes := make([]byte, 16)
		verificationGasLimit.FillBytes(verificationGasBytes)
		copy(result[0:16], verificationGasBytes)
	}

	return result
}

// packGasFees packs maxFeePerGas and maxPriorityFeePerGas into a bytes32
// Solidity format: bytes32(uint256(maxPriorityFeePerGas) << 128 | uint256(maxFeePerGas))
// bytes 0-15 (high 128 bits): maxPriorityFeePerGas
// bytes 16-31 (low 128 bits): maxFeePerGas
func packGasFees(maxFeePerGas, maxPriorityFeePerGas *big.Int) [32]byte {
	var result [32]byte

	// Pack maxFeePerGas into lower 16 bytes (bytes 16-31, right-aligned)
	if maxFeePerGas != nil {
		maxFeeBytes := make([]byte, 16)
		maxFeePerGas.FillBytes(maxFeeBytes)
		copy(result[16:32], maxFeeBytes)
	}

	// Pack maxPriorityFeePerGas into upper 16 bytes (bytes 0-15, right-aligned)
	if maxPriorityFeePerGas != nil {
		maxPriorityFeeBytes := make([]byte, 16)
		maxPriorityFeePerGas.FillBytes(maxPriorityFeeBytes)
		copy(result[0:16], maxPriorityFeeBytes)
	}

	return result
}

// unpackAccountGasLimits unpacks callGasLimit and verificationGasLimit from a bytes32
// Reverse of packAccountGasLimits: bytes 0-15 = verificationGasLimit (high 128 bits), bytes 16-31 = callGasLimit (low 128 bits)
func unpackAccountGasLimits(packed [32]byte) (*big.Int, *big.Int) {
	verificationGasLimit := new(big.Int).SetBytes(packed[0:16])
	callGasLimit := new(big.Int).SetBytes(packed[16:32])
	return callGasLimit, verificationGasLimit
}

// unpackGasFees unpacks maxFeePerGas and maxPriorityFeePerGas from a bytes32
// Reverse of packGasFees: bytes 0-15 = maxPriorityFeePerGas (high 128 bits), bytes 16-31 = maxFeePerGas (low 128 bits)
func unpackGasFees(packed [32]byte) (*big.Int, *big.Int) {
	maxPriorityFeePerGas := new(big.Int).SetBytes(packed[0:16])
	maxFeePerGas := new(big.Int).SetBytes(packed[16:32])
	return maxFeePerGas, maxPriorityFeePerGas
}

// DecodeHandleOps decodes the calldata for EntryPoint.handleOps()
// Returns the UserOperations and beneficiary address
func DecodeHandleOps(calldata []byte) ([]*models.UserOperation, common.Address, error) {
	// Check if calldata has at least 4 bytes (function selector)
	if len(calldata) < 4 {
		return nil, common.Address{}, fmt.Errorf("calldata too short: %d bytes", len(calldata))
	}

	// Verify it's handleOps call
	selector := calldata[:4]
	expectedSelector := GetHandleOpsSelector()
	if !bytes.Equal(selector, expectedSelector[:4]) {
		return nil, common.Address{}, fmt.Errorf("not a handleOps call: selector mismatch")
	}

	// Unpack the function arguments
	method, exists := entryPointABIParsed.Methods["handleOps"]
	if !exists {
		return nil, common.Address{}, fmt.Errorf("handleOps method not found in ABI")
	}

	// Unpack returns []interface{} with the arguments
	args, err := method.Inputs.Unpack(calldata[4:])
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("failed to unpack handleOps arguments: %w", err)
	}

	if len(args) != 2 {
		return nil, common.Address{}, fmt.Errorf("expected 2 arguments, got %d", len(args))
	}

	// First argument is []PackedUserOperationABI
	// The ABI package returns anonymous structs, so we need to use reflection to convert
	var opsInterface []PackedUserOperationABI
	
	// Use reflection to handle the anonymous struct slice returned by ABI unpacking
	argValue := reflect.ValueOf(args[0])
	if argValue.Kind() != reflect.Slice {
		return nil, common.Address{}, fmt.Errorf("first argument is not a slice, got %T", args[0])
	}
	
	opsInterface = make([]PackedUserOperationABI, 0, argValue.Len())
	for i := 0; i < argValue.Len(); i++ {
		elem := argValue.Index(i)
		if elem.Kind() == reflect.Interface {
			elem = elem.Elem()
		}
		
		// Handle both struct and map[string]interface{} cases
		var op PackedUserOperationABI
		if elem.Kind() == reflect.Struct {
			// Anonymous struct case - use reflection to extract fields
			elemType := elem.Type()
			opValue := reflect.ValueOf(&op).Elem()
			
			for j := 0; j < elemType.NumField(); j++ {
				field := elemType.Field(j)
				fieldValue := elem.Field(j)
				opField := opValue.FieldByName(field.Name)
				
				if !opField.IsValid() || !opField.CanSet() {
					continue
				}
				
				// Convert the field value to the target type
				if fieldValue.Kind() == reflect.Interface {
					fieldValue = fieldValue.Elem()
				}
				
				// Try direct assignment first
				if fieldValue.Type().AssignableTo(opField.Type()) {
					opField.Set(fieldValue)
					continue
				}
				
				// Handle specific type conversions
				if opField.Type().Kind() == reflect.Array && fieldValue.Kind() == reflect.Array {
					// For [32]byte arrays
					if opField.Type().Elem().Kind() == reflect.Uint8 && fieldValue.Type().Elem().Kind() == reflect.Uint8 {
						if opField.Type().Len() == fieldValue.Type().Len() {
							reflect.Copy(opField, fieldValue)
						}
					}
				} else if opField.Type().Kind() == reflect.Slice && fieldValue.Kind() == reflect.Slice {
					// For []byte slices
					if opField.Type().Elem().Kind() == reflect.Uint8 && fieldValue.Type().Elem().Kind() == reflect.Uint8 {
						opField.Set(fieldValue)
					}
				} else if opField.Type() == reflect.TypeOf((*big.Int)(nil)).Elem() {
					// For *big.Int - check if assignable
					if fieldValue.Type().AssignableTo(opField.Type()) {
						opField.Set(fieldValue)
					}
				} else if opField.Type() == reflect.TypeOf(common.Address{}) {
					// For common.Address - check if assignable
					if fieldValue.Type().AssignableTo(opField.Type()) {
						opField.Set(fieldValue)
					}
				}
			}
		} else if elem.Kind() == reflect.Map {
			// Map case - extract from map[string]interface{}
			opMap := elem.Interface().(map[string]interface{})
			if sender, ok := opMap["sender"].(common.Address); ok {
				op.Sender = sender
			}
			if nonce, ok := opMap["nonce"].(*big.Int); ok {
				op.Nonce = nonce
			}
			if initCode, ok := opMap["initCode"].([]byte); ok {
				op.InitCode = initCode
			}
			if callData, ok := opMap["callData"].([]byte); ok {
				op.CallData = callData
			}
			if accountGasLimits, ok := opMap["accountGasLimits"].([32]byte); ok {
				op.AccountGasLimits = accountGasLimits
			}
			if preVerificationGas, ok := opMap["preVerificationGas"].(*big.Int); ok {
				op.PreVerificationGas = preVerificationGas
			}
			if gasFees, ok := opMap["gasFees"].([32]byte); ok {
				op.GasFees = gasFees
			}
			if paymasterAndData, ok := opMap["paymasterAndData"].([]byte); ok {
				op.PaymasterAndData = paymasterAndData
			}
			if signature, ok := opMap["signature"].([]byte); ok {
				op.Signature = signature
			}
		} else {
			return nil, common.Address{}, fmt.Errorf("unexpected element type in ops array: %T (kind: %v)", elem.Interface(), elem.Kind())
		}
		
		opsInterface = append(opsInterface, op)
	}

	// Second argument is beneficiary address
	beneficiary, ok := args[1].(common.Address)
	if !ok {
		return nil, common.Address{}, fmt.Errorf("second argument is not an address, got %T", args[1])
	}

	// Convert PackedUserOperationABI to models.UserOperation
	userOps := make([]*models.UserOperation, 0, len(opsInterface))
	for _, op := range opsInterface {
		// Unpack the packed fields
		callGasLimit, verificationGasLimit := unpackAccountGasLimits(op.AccountGasLimits)
		maxFeePerGas, maxPriorityFeePerGas := unpackGasFees(op.GasFees)

		userOp := &models.UserOperation{
			Sender:               op.Sender,
			Nonce:                op.Nonce,
			InitCode:             op.InitCode,
			CallData:             op.CallData,
			CallGasLimit:         callGasLimit,
			VerificationGasLimit: verificationGasLimit,
			PreVerificationGas:   op.PreVerificationGas,
			MaxFeePerGas:         maxFeePerGas,
			MaxPriorityFeePerGas: maxPriorityFeePerGas,
			PaymasterAndData:     op.PaymasterAndData,
			Signature:            op.Signature,
		}

		userOps = append(userOps, userOp)
	}

	return userOps, beneficiary, nil
}

// EncodeSimulateValidation encodes the calldata for EntryPoint.simulateValidation()
// This function uses the standard UserOperation format (for EntryPoint v0.6)
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
		VerificationGasLimit: userOp.VerificationGasLimit,
		PreVerificationGas:   userOp.PreVerificationGas,
		MaxFeePerGas:         userOp.MaxFeePerGas,
		MaxPriorityFeePerGas: userOp.MaxPriorityFeePerGas,
		PaymasterAndData:     userOp.PaymasterAndData,
		Signature:            userOp.Signature,
	}

	// Encode the function call
	data, err := entryPointABIParsed.Pack("simulateValidation", op)
	if err != nil {
		return nil, fmt.Errorf("failed to encode simulateValidation: %w", err)
	}

	return data, nil
}

// ValidationResult structs for EntryPoint v0.9.0 simulateValidation return value
// These match the structs defined in IEntryPointSimulations.sol
type ReturnInfo struct {
	PreOpGas                *big.Int
	Prefund                 *big.Int
	AccountValidationData   *big.Int
	PaymasterValidationData *big.Int
	PaymasterContext        []byte
}

type StakeInfo struct {
	Stake           *big.Int
	UnstakeDelaySec *big.Int
}

type AggregatorStakeInfo struct {
	Aggregator common.Address
	StakeInfo  StakeInfo
}

type ValidationResult struct {
	ReturnInfo      ReturnInfo
	SenderInfo      StakeInfo
	FactoryInfo     StakeInfo
	PaymasterInfo   StakeInfo
	AggregatorInfo  AggregatorStakeInfo
}

// DecodeValidationResult decodes the ValidationResult struct from eth_call return data
// EntryPoint v0.9.0 simulateValidation returns ValidationResult normally (not via revert)
func DecodeValidationResult(returnData []byte) (*ValidationResult, error) {
	// Get the simulateValidation method from EntryPointSimulations ABI
	method, exists := entryPointSimulationsABIParsed.Methods["simulateValidation"]
	if !exists {
		return nil, fmt.Errorf("simulateValidation method not found in EntryPointSimulations ABI")
	}

	// Unpack the return data using the method's output definition
	// The return data is already ABI-encoded, so we can unpack it directly
	unpacked, err := method.Outputs.Unpack(returnData)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack ValidationResult: %w", err)
	}

	if len(unpacked) != 1 {
		return nil, fmt.Errorf("expected 1 return value, got %d", len(unpacked))
	}

	// The unpacked value is a struct, we need to convert it to our ValidationResult type
	// The ABI package returns anonymous structs, so we use reflection to extract fields
	resultValue := reflect.ValueOf(unpacked[0])
	if resultValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %T", unpacked[0])
	}

	// Extract fields from the anonymous struct
	// ValidationResult has 5 fields: returnInfo, senderInfo, factoryInfo, paymasterInfo, aggregatorInfo
	result := &ValidationResult{}

	// Helper to extract field value, handling interface{} wrapping
	extractField := func(v reflect.Value, fieldIdx int) reflect.Value {
		field := v.Field(fieldIdx)
		if field.Kind() == reflect.Interface {
			field = field.Elem()
		}
		return field
	}

	// returnInfo (ReturnInfo struct)
	returnInfoValue := extractField(resultValue, 0)
	if returnInfoValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("returnInfo is not a struct, got %v", returnInfoValue.Kind())
	}
	returnInfoField0 := extractField(returnInfoValue, 0)
	returnInfoField1 := extractField(returnInfoValue, 1)
	returnInfoField2 := extractField(returnInfoValue, 2)
	returnInfoField3 := extractField(returnInfoValue, 3)
	returnInfoField4 := extractField(returnInfoValue, 4)
	result.ReturnInfo = ReturnInfo{
		PreOpGas:                returnInfoField0.Interface().(*big.Int),
		Prefund:                 returnInfoField1.Interface().(*big.Int),
		AccountValidationData:   returnInfoField2.Interface().(*big.Int),
		PaymasterValidationData: returnInfoField3.Interface().(*big.Int),
		PaymasterContext:        returnInfoField4.Interface().([]byte),
	}

	// senderInfo (StakeInfo struct)
	senderInfoValue := extractField(resultValue, 1)
	if senderInfoValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("senderInfo is not a struct, got %v", senderInfoValue.Kind())
	}
	senderInfoField0 := extractField(senderInfoValue, 0)
	senderInfoField1 := extractField(senderInfoValue, 1)
	result.SenderInfo = StakeInfo{
		Stake:           senderInfoField0.Interface().(*big.Int),
		UnstakeDelaySec: senderInfoField1.Interface().(*big.Int),
	}

	// factoryInfo (StakeInfo struct)
	factoryInfoValue := extractField(resultValue, 2)
	if factoryInfoValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("factoryInfo is not a struct, got %v", factoryInfoValue.Kind())
	}
	factoryInfoField0 := extractField(factoryInfoValue, 0)
	factoryInfoField1 := extractField(factoryInfoValue, 1)
	result.FactoryInfo = StakeInfo{
		Stake:           factoryInfoField0.Interface().(*big.Int),
		UnstakeDelaySec: factoryInfoField1.Interface().(*big.Int),
	}

	// paymasterInfo (StakeInfo struct)
	paymasterInfoValue := extractField(resultValue, 3)
	if paymasterInfoValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("paymasterInfo is not a struct, got %v", paymasterInfoValue.Kind())
	}
	paymasterInfoField0 := extractField(paymasterInfoValue, 0)
	paymasterInfoField1 := extractField(paymasterInfoValue, 1)
	result.PaymasterInfo = StakeInfo{
		Stake:           paymasterInfoField0.Interface().(*big.Int),
		UnstakeDelaySec: paymasterInfoField1.Interface().(*big.Int),
	}

	// aggregatorInfo (AggregatorStakeInfo struct)
	aggregatorInfoValue := extractField(resultValue, 4)
	if aggregatorInfoValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("aggregatorInfo is not a struct, got %v", aggregatorInfoValue.Kind())
	}
	aggregatorField0 := extractField(aggregatorInfoValue, 0) // aggregator address
	aggregatorStakeInfoValue := extractField(aggregatorInfoValue, 1) // stakeInfo field
	if aggregatorStakeInfoValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("aggregatorInfo.stakeInfo is not a struct, got %v", aggregatorStakeInfoValue.Kind())
	}
	aggregatorStakeField0 := extractField(aggregatorStakeInfoValue, 0)
	aggregatorStakeField1 := extractField(aggregatorStakeInfoValue, 1)
	result.AggregatorInfo = AggregatorStakeInfo{
		Aggregator: aggregatorField0.Interface().(common.Address),
		StakeInfo: StakeInfo{
			Stake:           aggregatorStakeField0.Interface().(*big.Int),
			UnstakeDelaySec: aggregatorStakeField1.Interface().(*big.Int),
		},
	}

	return result, nil
}

// EncodeSimulateValidationPacked encodes the calldata for simulateValidation() using PackedUserOperation format
// Uses EntryPointSimulations ABI since that's the ABI that includes simulateValidation
// Note: Even when calling EntryPoint directly, we use EntryPointSimulations ABI for encoding
// because the function signature is the same (both use PackedUserOperation format)
func EncodeSimulateValidationPacked(userOp *models.UserOperation) ([]byte, error) {
	packedOp := PackedUserOperationABI{
		Sender:             userOp.Sender,
		Nonce:              userOp.Nonce,
		InitCode:           userOp.InitCode,
		CallData:           userOp.CallData,
		AccountGasLimits:   packAccountGasLimits(userOp.CallGasLimit, userOp.VerificationGasLimit),
		PreVerificationGas: userOp.PreVerificationGas,
		GasFees:            packGasFees(userOp.MaxFeePerGas, userOp.MaxPriorityFeePerGas),
		PaymasterAndData:   userOp.PaymasterAndData,
		Signature:          userOp.Signature,
	}

	// Use EntryPointSimulations ABI - it has simulateValidation method
	// This works for both EntryPoint and EntryPointSimulations since they have the same signature
	data, err := entryPointSimulationsABIParsed.Pack("simulateValidation", packedOp)
	if err != nil {
		return nil, fmt.Errorf("failed to encode simulateValidation (packed): %w", err)
	}

	return data, nil
}

// EncodeGetDeposit encodes the calldata for EntryPoint.getDepositInfo()
// Note: EntryPoint v0.9.0 uses getDepositInfo instead of getDeposit
// getDepositInfo returns a struct with deposit, staked, stake, unstakeDelaySec, and withdrawTime
func EncodeGetDeposit(account common.Address) ([]byte, error) {
	data, err := entryPointABIParsed.Pack("getDepositInfo", account)
	if err != nil {
		return nil, fmt.Errorf("failed to encode getDepositInfo: %w", err)
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

// EncodeFactoryGetAddress encodes the calldata for SimpleAccountFactory.getAddress(owner, salt)
func EncodeFactoryGetAddress(owner common.Address, salt *big.Int) ([]byte, error) {
	if simpleAccountFactoryABIParsed == nil {
		return nil, fmt.Errorf("SimpleAccountFactory ABI not initialized")
	}
	data, err := simpleAccountFactoryABIParsed.Pack("getAddress", owner, salt)
	if err != nil {
		return nil, fmt.Errorf("failed to encode getAddress: %w", err)
	}
	return data, nil
}

// EncodeFactoryAccountImplementation encodes the calldata for SimpleAccountFactory.accountImplementation()
func EncodeFactoryAccountImplementation() ([]byte, error) {
	if simpleAccountFactoryABIParsed == nil {
		return nil, fmt.Errorf("SimpleAccountFactory ABI not initialized")
	}
	data, err := simpleAccountFactoryABIParsed.Pack("accountImplementation")
	if err != nil {
		return nil, fmt.Errorf("failed to encode accountImplementation: %w", err)
	}
	return data, nil
}

// GetHandleOpsSelector returns the 4-byte function selector for handleOps
// Derived from the ABI to ensure correctness
func GetHandleOpsSelector() []byte {
	method, exists := entryPointABIParsed.Methods["handleOps"]
	if !exists {
		panic("handleOps method not found in EntryPoint ABI")
	}
	return method.ID
}
