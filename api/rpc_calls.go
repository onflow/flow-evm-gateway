package api

const (
	// JSON-RPC calls under the `eth_` namespace
	EthBlockNumber                         = "BlockNumber"
	EthSyncing                             = "Syncing"
	EthSendRawTransaction                  = "SendRawTransaction"
	EthGetBalance                          = "GetBalance"
	EthGetTransactionByHash                = "GetTransactionByHash"
	EthGetTransactionByBlockHashAndIndex   = "GetTransactionByBlockHashAndIndex"
	EthGetTransactionByBlockNumberAndIndex = "GetTransactionByBlockNumberAndIndex"
	EthGetTransactionReceipt               = "GetTransactionReceipt"
	EthGetBlockByHash                      = "GetBlockByHash"
	EthGetBlockByNumber                    = "GetBlockByNumber"
	EthGetBlockReceipts                    = "GetBlockReceipts"
	EthGetBlockTransactionCountByHash      = "GetBlockTransactionCountByHash"
	EthGetBlockTransactionCountByNumber    = "GetBlockTransactionCountByNumber"
	EthCall                                = "Call"
	EthGetLogs                             = "GetLogs"
	EthGetTransactionCount                 = "GetTransactionCount"
	EthEstimateGas                         = "EstimateGas"
	EthGetCode                             = "GetCode"
	EthGetStorageAt                        = "GetStorageAt"
	EthNewPendingTransactionFilter         = "NewPendingTransactionFilter"
	EthNewBlockFilter                      = "NewBlockFilter"
	EthNewFilter                           = "NewFilter"
	EthGetFilterLogs                       = "GetFilterLogs"
	EthGetFilterChanges                    = "GetFilterChanges"
	EthFeeHistory                          = "FeeHistory"
	// ERC-4337 Bundler methods
	EthSendUserOperation           = "SendUserOperation"
	EthEstimateUserOperationGas    = "EstimateUserOperationGas"
	EthGetUserOperationByHash      = "GetUserOperationByHash"
	EthGetUserOperationReceipt    = "GetUserOperationReceipt"

	// JSON-RPC calls under the `debug_` namespace
	DebugTraceTransaction   = "TraceTransaction"
	DebugTraceBlockByNumber = "TraceBlockByNumber"
	DebugTraceBlockByHash   = "TraceBlockByHash"
	DebugTraceCall          = "TraceCall"
	DebugFlowHeightByBlock  = "FlowHeightByBlock"
)
