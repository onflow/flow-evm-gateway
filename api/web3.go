package api

import (
	"fmt"

	"github.com/onflow/go-ethereum/common/hexutil"
	"github.com/onflow/go-ethereum/crypto"
)

var Version = "development"

// Web3API offers helper utils
type Web3API struct{}

// ClientVersion returns the node name
func (s *Web3API) ClientVersion() string {
	return fmt.Sprintf("flow-evm-gateway@%s", Version)
}

// Sha3 applies the ethereum sha3 implementation on the input.
// It assumes the input is hex encoded.
func (s *Web3API) Sha3(input hexutil.Bytes) hexutil.Bytes {
	return crypto.Keccak256(input)
}
