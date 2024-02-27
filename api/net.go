package api

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// NetAPI offers network related RPC methods
type NetAPI struct{}

// Listening returns an indication if the node is
// listening for network connections.
func (s *NetAPI) Listening() bool {
	return true // always listening
}

// PeerCount returns the number of connected peers
func (s *NetAPI) PeerCount() hexutil.Uint {
	return 1
}

// Version returns the current ethereum protocol version.
func (s *NetAPI) Version() string {
	return fmt.Sprintf("%d", 666)
}
