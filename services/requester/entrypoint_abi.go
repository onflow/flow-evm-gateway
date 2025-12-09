package requester

import (
	"bytes"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/onflow/flow-evm-gateway/models"
)

// EntryPointABI is the ABI for the ERC-4337 EntryPoint contract (v0.7+)
// Uses PackedUserOperation format
const entryPointABI = `[
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
	entryPointABIParsed, err = abi.JSON(bytes.NewReader([]byte(entryPointABI)))
	if err != nil {
		panic(fmt.Sprintf("failed to parse EntryPoint ABI: %v", err))
	}
	entryPointSimulationsABIParsed, err = abi.JSON(bytes.NewReader([]byte(entryPointSimulationsABI)))
	if err != nil {
		panic(fmt.Sprintf("failed to parse EntryPointSimulations ABI: %v", err))
	}
	parsed, err := abi.JSON(bytes.NewReader([]byte(simpleAccountFactoryABI)))
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

// EncodeSimulateValidationPacked encodes the calldata for EntryPointSimulations.simulateValidation()
// This function uses PackedUserOperation format (for EntryPointSimulations v0.7+)
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

	// Encode the function call using EntryPointSimulations ABI
	data, err := entryPointSimulationsABIParsed.Pack("simulateValidation", packedOp)
	if err != nil {
		return nil, fmt.Errorf("failed to encode simulateValidation (packed): %w", err)
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
