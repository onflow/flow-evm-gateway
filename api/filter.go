package api

import (
	"context"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
	"sync"
)

// filter defines a general resource filter that is used when pulling new data.
//
// Filters work by remembering the last height at which the data was pulled
// and allow on next request to fetch all data that is missing.
// Each filter is identified by a unique ID.
type filter interface {
	id() rpc.ID
	lastHeight() uint64
	updateHeight(uint64)
}

// heightBaseFilter is a base implementation that keeps track of
// last height and filter unique ID.
type heightBaseFilter struct {
	last  uint64
	rpcID rpc.ID
}

func (h *heightBaseFilter) lastHeight() uint64 {
	return h.last
}

func (h *heightBaseFilter) updateHeight(u uint64) {
	h.last = u
}

func (h *heightBaseFilter) id() rpc.ID {
	return h.rpcID
}

var _ filter = &blocksFilter{}
var _ filter = &transactionsFilter{}

// blocksFilter is used to get all new blocks since the last request
type blocksFilter struct {
	*heightBaseFilter
}

func newBlocksFilter(lastHeight uint64) *blocksFilter {
	return &blocksFilter{
		&heightBaseFilter{
			last:  lastHeight,
			rpcID: rpc.NewID(),
		},
	}
}

// transactionFilter is used to get all new transactions since last request.
//
// FullTx parameters determines if the result will include only
// hashes or full transaction body.
type transactionsFilter struct {
	*heightBaseFilter
	fullTx bool
}

func newTransactionFilter(lastHeight uint64, fullTx bool) *transactionsFilter {
	return &transactionsFilter{
		&heightBaseFilter{
			last:  lastHeight,
			rpcID: rpc.NewID(),
		},
		fullTx,
	}
}

// logsFilter is used to get all new logs since the last request.
//
// Criteria parameter filters the logs according to the criteria values.
type logsFilter struct {
	*heightBaseFilter
	criteria *filters.FilterCriteria
}

func newLogsFilter(lastHeight uint64, criteria *filters.FilterCriteria) *logsFilter {
	return &logsFilter{
		&heightBaseFilter{
			last:  lastHeight,
			rpcID: rpc.NewID(),
		},
		criteria,
	}
}

type FilterAPI struct {
	logger       zerolog.Logger
	config       *config.Config
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	accounts     storage.AccountIndexer

	// todo add timeout to clear filters and cap filter length to prevent OOM
	filters map[rpc.ID]filter
	mux     sync.RWMutex
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
		filters:      make(map[rpc.ID]filter),
	}
}

func (api *FilterAPI) addFilter(f filter) rpc.ID {
	api.mux.Lock()
	defer api.mux.Unlock()

	api.filters[f.id()] = f
	return f.id()
}

// NewPendingTransactionFilter creates a filter that fetches pending transactions
// as transactions enter the pending state.
//
// It is part of the filter package because this filter can be used through the
// `eth_getFilterChanges` polling method that is also used for log filters.
func (api *FilterAPI) NewPendingTransactionFilter(fullTx *bool) (rpc.ID, error) {
	last, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return "", err
	}

	full := false
	if fullTx != nil && *fullTx {
		full = true
	}

	return api.addFilter(newTransactionFilter(last, full)), nil
}

// NewBlockFilter creates a filter that fetches blocks that are imported into the chain.
// It is part of the filter package since polling goes with eth_getFilterChanges.
func (api *FilterAPI) NewBlockFilter() (rpc.ID, error) {
	// todo maybe we can optimize
	last, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return "", err
	}

	return api.addFilter(newBlocksFilter(last)), nil
}

func (api *FilterAPI) UninstallFilter(id rpc.ID) bool {
	api.mux.Lock()
	defer api.mux.Unlock()

	_, ok := api.filters[id]
	delete(api.filters, id)
	return ok
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
func (api *FilterAPI) NewFilter(criteria filters.FilterCriteria) (rpc.ID, error) {
	last, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return "", err
	}

	return api.addFilter(newLogsFilter(last, &criteria)), nil
}

// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found an empty array of logs is returned.
func (api *FilterAPI) GetFilterLogs(ctx context.Context, id rpc.ID) ([]*types.Log, error) {
	// todo this should call the normal get logs api
	panic("implement")
}

// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (api *FilterAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	panic("implement")
}
