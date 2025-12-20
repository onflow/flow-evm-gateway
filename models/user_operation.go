package models

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// UserOperation represents an ERC-4337 UserOperation
// See: https://eips.ethereum.org/EIPS/eip-4337
type UserOperation struct {
	Sender               common.Address `json:"sender"`
	Nonce               *big.Int       `json:"nonce"`
	InitCode            []byte         `json:"initCode"`
	CallData            []byte         `json:"callData"`
	CallGasLimit        *big.Int       `json:"callGasLimit"`
	VerificationGasLimit *big.Int      `json:"verificationGasLimit"`
	PreVerificationGas  *big.Int       `json:"preVerificationGas"`
	MaxFeePerGas        *big.Int       `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *big.Int      `json:"maxPriorityFeePerGas"`
	PaymasterAndData    []byte         `json:"paymasterAndData"`
	Signature           []byte         `json:"signature"`
}

// Hash computes the UserOperation hash per EntryPoint v0.9.0 spec
// The hash is computed as: keccak256(keccak256(packedUserOp) || entryPoint || chainId)
func (uo *UserOperation) Hash(entryPoint common.Address, chainID *big.Int) (common.Hash, error) {
	// First, pack only the UserOp fields (without entryPoint and chainID)
	packedUserOp, err := uo.PackForSignature()
	if err != nil {
		return common.Hash{}, err
	}

	// Hash the packed UserOp
	packedUserOpHash := crypto.Keccak256Hash(packedUserOp)

	// Now pack: keccak256(packedUserOp) || entryPoint || chainId
	var finalPacked []byte
	finalPacked = append(finalPacked, packedUserOpHash.Bytes()...)

	// entryPoint (20 bytes)
	finalPacked = append(finalPacked, entryPoint.Bytes()...)

	// chainId (32 bytes, big-endian)
	chainIDBytes := make([]byte, 32)
	if chainID != nil {
		chainID.FillBytes(chainIDBytes)
	}
	finalPacked = append(finalPacked, chainIDBytes...)

	// Final hash
	return crypto.Keccak256Hash(finalPacked), nil
}

// PackForSignature packs the UserOperation fields for signature verification
// This packs only the UserOp fields (without entryPoint and chainID)
// EntryPoint v0.9.0 format: abi.encodePacked(
//   sender,
//   nonce,
//   keccak256(initCode),
//   keccak256(callData),
//   callGasLimit,
//   verificationGasLimit,
//   preVerificationGas,
//   maxFeePerGas,
//   maxPriorityFeePerGas,
//   keccak256(paymasterAndData)
// )
func (uo *UserOperation) PackForSignature() ([]byte, error) {
	var packed []byte

	// sender (20 bytes)
	packed = append(packed, uo.Sender.Bytes()...)

	// nonce (32 bytes, big-endian)
	nonceBytes := make([]byte, 32)
	if uo.Nonce != nil {
		uo.Nonce.FillBytes(nonceBytes)
	}
	packed = append(packed, nonceBytes...)

	// keccak256(initCode) (32 bytes)
	initCodeHash := crypto.Keccak256Hash(uo.InitCode)
	packed = append(packed, initCodeHash.Bytes()...)

	// keccak256(callData) (32 bytes)
	callDataHash := crypto.Keccak256Hash(uo.CallData)
	packed = append(packed, callDataHash.Bytes()...)

	// callGasLimit (32 bytes)
	callGasBytes := make([]byte, 32)
	if uo.CallGasLimit != nil {
		uo.CallGasLimit.FillBytes(callGasBytes)
	}
	packed = append(packed, callGasBytes...)

	// verificationGasLimit (32 bytes)
	verificationGasBytes := make([]byte, 32)
	if uo.VerificationGasLimit != nil {
		uo.VerificationGasLimit.FillBytes(verificationGasBytes)
	}
	packed = append(packed, verificationGasBytes...)

	// preVerificationGas (32 bytes)
	preVerificationGasBytes := make([]byte, 32)
	if uo.PreVerificationGas != nil {
		uo.PreVerificationGas.FillBytes(preVerificationGasBytes)
	}
	packed = append(packed, preVerificationGasBytes...)

	// maxFeePerGas (32 bytes)
	maxFeeBytes := make([]byte, 32)
	if uo.MaxFeePerGas != nil {
		uo.MaxFeePerGas.FillBytes(maxFeeBytes)
	}
	packed = append(packed, maxFeeBytes...)

	// maxPriorityFeePerGas (32 bytes)
	maxPriorityFeeBytes := make([]byte, 32)
	if uo.MaxPriorityFeePerGas != nil {
		uo.MaxPriorityFeePerGas.FillBytes(maxPriorityFeeBytes)
	}
	packed = append(packed, maxPriorityFeeBytes...)

	// keccak256(paymasterAndData) (32 bytes)
	paymasterHash := crypto.Keccak256Hash(uo.PaymasterAndData)
	packed = append(packed, paymasterHash.Bytes()...)

	return packed, nil
}

// VerifySignature verifies the UserOperation signature
func (uo *UserOperation) VerifySignature(entryPoint common.Address, chainID *big.Int) (bool, error) {
	if len(uo.Signature) < 65 {
		return false, fmt.Errorf("signature too short: %d bytes", len(uo.Signature))
	}

	// Get the hash to sign
	hash, err := uo.Hash(entryPoint, chainID)
	if err != nil {
		return false, err
	}

	// Recover the public key from signature
	// ERC-4337 uses EIP-191 style signing: keccak256("\x19\x01" || chainId || userOpHash)
	// IMPORTANT: For EIP-191, chainID is encoded as variable-length bytes (standard practice)
	// This matches what standard libraries (ethers.js, viem, etc.) do
	// Note: The UserOp hash uses 32-byte padded chainID (EntryPoint v0.9.0 format),
	// but the signature hash (EIP-191) uses variable-length chainID encoding
	sigHash := crypto.Keccak256Hash(
		[]byte("\x19\x01"),
		chainID.Bytes(), // Variable-length encoding (standard EIP-191)
		hash.Bytes(),
	)

	// Extract v from signature
	v := uint(uo.Signature[64])

	// Recover public key
	pubKey, err := crypto.SigToPub(sigHash.Bytes(), append(uo.Signature[:64], byte(v)))
	if err != nil {
		return false, err
	}

	// Get address from public key
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)

	// Verify it matches the sender
	return recoveredAddr == uo.Sender, nil
}

// EncodeRLP encodes the UserOperation for RLP encoding
// This is used when constructing EntryPoint.handleOps() calldata
func (uo *UserOperation) EncodeRLP() ([]byte, error) {
	return rlp.EncodeToBytes(uo)
}

// DecodeRLP decodes a UserOperation from RLP-encoded data
func DecodeUserOperation(data []byte) (*UserOperation, error) {
	var uo UserOperation
	if err := rlp.DecodeBytes(data, &uo); err != nil {
		return nil, err
	}
	return &uo, nil
}

// UserOperationArgs represents the JSON-RPC arguments for UserOperation
type UserOperationArgs struct {
	Sender               common.Address  `json:"sender"`
	Nonce                *hexutil.Big    `json:"nonce"`
	InitCode             *hexutil.Bytes  `json:"initCode,omitempty"`
	CallData             *hexutil.Bytes  `json:"callData"`
	CallGasLimit         *hexutil.Big    `json:"callGasLimit"`
	VerificationGasLimit *hexutil.Big    `json:"verificationGasLimit"`
	PreVerificationGas   *hexutil.Big    `json:"preVerificationGas"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	PaymasterAndData     *hexutil.Bytes  `json:"paymasterAndData,omitempty"`
	Signature            *hexutil.Bytes   `json:"signature"`
}

// ToUserOperation converts UserOperationArgs to UserOperation
func (args *UserOperationArgs) ToUserOperation() (*UserOperation, error) {
	uo := &UserOperation{
		Sender: args.Sender,
	}

	if args.Nonce != nil {
		uo.Nonce = args.Nonce.ToInt()
	} else {
		uo.Nonce = big.NewInt(0)
	}

	if args.InitCode != nil {
		uo.InitCode = *args.InitCode
	}

	if args.CallData == nil {
		return nil, fmt.Errorf("callData is required")
	}
	uo.CallData = *args.CallData

	if args.CallGasLimit == nil {
		return nil, fmt.Errorf("callGasLimit is required")
	}
	uo.CallGasLimit = args.CallGasLimit.ToInt()

	if args.VerificationGasLimit == nil {
		return nil, fmt.Errorf("verificationGasLimit is required")
	}
	uo.VerificationGasLimit = args.VerificationGasLimit.ToInt()

	if args.PreVerificationGas == nil {
		return nil, fmt.Errorf("preVerificationGas is required")
	}
	uo.PreVerificationGas = args.PreVerificationGas.ToInt()

	if args.MaxFeePerGas == nil {
		return nil, fmt.Errorf("maxFeePerGas is required")
	}
	uo.MaxFeePerGas = args.MaxFeePerGas.ToInt()

	if args.MaxPriorityFeePerGas == nil {
		return nil, fmt.Errorf("maxPriorityFeePerGas is required")
	}
	uo.MaxPriorityFeePerGas = args.MaxPriorityFeePerGas.ToInt()

	if args.PaymasterAndData != nil {
		uo.PaymasterAndData = *args.PaymasterAndData
	}

	if args.Signature == nil {
		return nil, fmt.Errorf("signature is required")
	}
	uo.Signature = *args.Signature

	return uo, nil
}

