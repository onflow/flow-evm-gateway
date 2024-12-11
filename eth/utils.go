package eth

// A map containing all the valid method names that are found
// in the Ethereum JSON-RPC API specification.
// Update accordingly if any new methods are added/removed.
var validMethods = map[string]struct{}{
	// eth namespace
	"eth_blockNumber":                         {},
	"eth_syncing":                             {},
	"eth_sendRawTransaction":                  {},
	"eth_getBalance":                          {},
	"eth_getTransactionByHash":                {},
	"eth_getTransactionByBlockHashAndIndex":   {},
	"eth_getTransactionByBlockNumberAndIndex": {},
	"eth_getTransactionReceipt":               {},
	"eth_getBlockByHash":                      {},
	"eth_getBlockByNumber":                    {},
	"eth_getBlockReceipts":                    {},
	"eth_getBlockTransactionCountByHash":      {},
	"eth_getBlockTransactionCountByNumber":    {},
	"eth_call":                                {},
	"eth_getLogs":                             {},
	"eth_getTransactionCount":                 {},
	"eth_estimateGas":                         {},
	"eth_getCode":                             {},
	"eth_feeHistory":                          {},
	"eth_getStorageAt":                        {},
	"eth_chainId":                             {},
	"eth_coinbase":                            {},
	"eth_gasPrice":                            {},
	"eth_getUncleCountByBlockHash":            {},
	"eth_getUncleCountByBlockNumber":          {},
	"eth_getUncleByBlockHashAndIndex":         {},
	"eth_getUncleByBlockNumberAndIndex":       {},
	"eth_maxPriorityFeePerGas":                {},
	"eth_mining":                              {},
	"eth_hashrate":                            {},
	"eth_getProof":                            {},
	"eth_createAccessList":                    {},

	// debug namespace
	"debug_traceTransaction":   {},
	"debug_traceBlockByNumber": {},
	"debug_traceBlockByHash":   {},
	"debug_traceCall":          {},
	"debug_flowHeightByBlock":  {},

	// web3 namespace
	"web3_clientVersion": {},
	"web3_sha3":          {},

	// net namespace
	"net_listening": {},
	"net_peerCount": {},
	"net_version":   {},

	// txpool namespace
	"txpool_content":     {},
	"txpool_contentFrom": {},
	"txpool_status":      {},
	"txpool_inspect":     {},
}

// Returns whether the given method name is a valid method from
// the Ethereum JSON-RPC API specification.
func IsValidMethod(methodName string) bool {
	_, ok := validMethods[methodName]
	return ok
}
