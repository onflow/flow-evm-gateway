package api

import (
	"context"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
)

type FilterAPI struct {
	logger       zerolog.Logger
	config       *config.Config
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	accounts     storage.AccountIndexer

	filters map[any]any
}

func NewFilterAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
	accounts storage.AccountIndexer,
) *FilterAPI {
	return &FilterAPI{
		logger:       logger,
		config:       config,
		blocks:       blocks,
		transactions: transactions,
		receipts:     receipts,
		accounts:     accounts,
		filters:      make(map[any]any),
	}
}

// NewPendingTransactionFilter creates a filter that fetches pending transactions
// as transactions enter the pending state.
//
// It is part of the filter package because this filter can be used through the
// `eth_getFilterChanges` polling method that is also used for log filters.
func (api *FilterAPI) NewPendingTransactionFilter(fullTx *bool) rpc.ID {

}

// NewBlockFilter creates a filter that fetches blocks that are imported into the chain.
// It is part of the filter package since polling goes with eth_getFilterChanges.
func (api *FilterAPI) NewBlockFilter() rpc.ID {

}

// NewFilter creates a new filter and returns the filter id. It can be
// used to retrieve logs when the state changes. This method cannot be
// used to fetch logs that are already stored in the state.
//
// Default criteria for the from and to block are "latest".
// Using "latest" as block number will return logs for mined blocks.
// Using "pending" as block number returns logs for not yet mined (pending) blocks.
// In case logs are removed (chain reorg) previously returned logs are returned
// again but with the removed property set to true.
//
// In case "fromBlock" > "toBlock" an error is returned.
func (api *FilterAPI) NewFilter(crit filters.FilterCriteria) (rpc.ID, error) {

}

// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found an empty array of logs is returned.
func (api *FilterAPI) GetFilterLogs(ctx context.Context, id rpc.ID) ([]*types.Log, error) {

}

// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (api *FilterAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {

}
