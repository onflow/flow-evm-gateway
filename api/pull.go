package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/rs/zerolog"
	"math/big"
	"sync"
	"time"
)

// filter defines a general resource filter that is used when pulling new data.
//
// Filters work by remembering the last height at which the data was pulled
// and allow on next request to fetch all data that is missing.
// Each filter is identified by a unique ID.
type filter interface {
	id() rpc.ID
	updateUsed(currentHeight uint64)
	next() uint64
	expired() bool
}

// baseFilter is a base implementation that keeps track of
// last height and filter unique ID.
type baseFilter struct {
	nextHeight uint64
	rpcID      rpc.ID
	used       time.Time
	expiry     time.Duration
}

func newBaseFilter(expiry time.Duration, currentHeight uint64) *baseFilter {
	if expiry == 0 {
		expiry = time.Minute * 5 // overwrite default expiry
	}

	return &baseFilter{
		nextHeight: currentHeight,
		rpcID:      rpc.NewID(),
		used:       time.Now(),
		expiry:     expiry,
	}
}

func (h *baseFilter) id() rpc.ID {
	return h.rpcID
}

// updateUsed updates the latest height the filter was used with as well as
// resets the time expiry for the filter.
func (h *baseFilter) updateUsed(currentHeight uint64) {
	h.nextHeight = currentHeight + 1
	h.used = time.Now()
}

func (h *baseFilter) next() uint64 {
	return h.nextHeight
}

func (h *baseFilter) expired() bool {
	return time.Since(h.used) > h.expiry
}

var _ filter = &blocksFilter{}
var _ filter = &transactionsFilter{}

// blocksFilter is used to get all new blocks since the last request
type blocksFilter struct {
	*baseFilter
}

func newBlocksFilter(expiry time.Duration, latestHeight uint64) *blocksFilter {
	return &blocksFilter{
		newBaseFilter(expiry, latestHeight),
	}
}

// transactionFilter is used to get all new transactions since last request.
//
// FullTx parameters determines if the result will include only
// hashes or full transaction body.
type transactionsFilter struct {
	*baseFilter
	fullTx bool
}

func newTransactionFilter(expiry time.Duration, latestHeight uint64, fullTx bool) *transactionsFilter {
	return &transactionsFilter{
		newBaseFilter(expiry, latestHeight),
		fullTx,
	}
}

// logsFilter is used to get all new logs since the last request.
//
// Criteria parameter filters the logs according to the criteria values.
type logsFilter struct {
	*baseFilter
	criteria *filters.FilterCriteria
}

func newLogsFilter(
	expiry time.Duration,
	latestHeight uint64,
	criteria *filters.FilterCriteria,
) *logsFilter {
	return &logsFilter{
		newBaseFilter(expiry, latestHeight),
		criteria,
	}
}

type PullAPI struct {
	logger       zerolog.Logger
	config       *config.Config
	blocks       storage.BlockIndexer
	transactions storage.TransactionIndexer
	receipts     storage.ReceiptIndexer
	filters      map[rpc.ID]filter
	mux          sync.RWMutex
}

func NewPullAPI(
	logger zerolog.Logger,
	config *config.Config,
	blocks storage.BlockIndexer,
	transactions storage.TransactionIndexer,
	receipts storage.ReceiptIndexer,
) *PullAPI {
	api := &PullAPI{
		logger:       logger,
		config:       config,
		blocks:       blocks,
		transactions: transactions,
		receipts:     receipts,
		filters:      make(map[rpc.ID]filter),
	}

	go api.filterExpiryChecker()

	return api
}

// NewPendingTransactionFilter creates a filter that fetches pending transactions
// as transactions enter the pending state.
//
// It is part of the filter package because this filter can be used through the
// `eth_getFilterChanges` polling method that is also used for log filters.
func (api *PullAPI) NewPendingTransactionFilter(fullTx *bool) (rpc.ID, error) {
	last, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return "", err
	}

	full := false
	if fullTx != nil && *fullTx {
		full = true
	}

	f := newTransactionFilter(api.config.FilterExpiry, last, full)
	return api.addFilter(f), nil
}

// NewBlockFilter creates a filter that fetches blocks that are imported into the chain.
// It is part of the filter package since polling goes with eth_getFilterChanges.
func (api *PullAPI) NewBlockFilter() (rpc.ID, error) {
	last, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return "", err
	}

	f := newBlocksFilter(api.config.FilterExpiry, last)
	return api.addFilter(f), nil
}

func (api *PullAPI) UninstallFilter(id rpc.ID) bool {
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
func (api *PullAPI) NewFilter(criteria filters.FilterCriteria) (rpc.ID, error) {
	latest, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return "", err
	}

	// if from block is actually set we use it as from value otherwise use latest
	if criteria.FromBlock != nil && criteria.FromBlock.Int64() >= 0 {
		latest = criteria.FromBlock.Uint64()
		// if to block is set and doesn't have a special value
		// (e.g. latest which is less than 0) make sure it's not less than from block
		if criteria.ToBlock != nil &&
			criteria.FromBlock.Cmp(criteria.ToBlock) > 0 &&
			criteria.ToBlock.Int64() > 0 {
			return "", fmt.Errorf("from block must be lower than to block")
		}
		// todo we should check for max range of from-to heights
	}

	f := newLogsFilter(api.config.FilterExpiry, latest, &criteria)
	return api.addFilter(f), nil
}

// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found an empty array of logs is returned.
func (api *PullAPI) GetFilterLogs(ctx context.Context, id rpc.ID) ([]*gethTypes.Log, error) {
	panic("not implemented")
}

// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (api *PullAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	api.mux.RLock()
	defer api.mux.RUnlock()

	f, ok := api.filters[id]
	if !ok {
		return nil, errors.Join(errs.ErrNotFound, fmt.Errorf("filted by id %s does not exist", id))
	}

	current, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	if f.expired() {
		api.UninstallFilter(id)
		return nil, errors.Join(errs.ErrNotFound, fmt.Errorf("filted by id %s expired", id))
	}
	defer f.updateUsed(current)

	switch f.(type) {
	case *blocksFilter:
		return api.getBlocks(current, f.(*blocksFilter))
	case *transactionsFilter:
		return api.getTransactions(current, f.(*transactionsFilter))
	case *logsFilter:
		return api.getLogs(current, f.(*logsFilter))
	}

	return nil, nil
}

// filterExpiryChecker continuously monitors all the registered filters and
// if any of them has been idle for too long i.e. no requests have been made
// for a period defined as filterExpiry using the filter ID it will be removed.
func (api *PullAPI) filterExpiryChecker() {
	for _ = range time.Tick(api.config.FilterExpiry) {
		for id, f := range api.filters {
			if f.expired() {
				api.UninstallFilter(id)
			}
		}
	}
}

func (api *PullAPI) addFilter(f filter) rpc.ID {
	api.mux.Lock()
	defer api.mux.Unlock()

	// we limit max active filters at any time to prevent abuse and OOM
	const maxFilters = 10_000
	if len(api.filters) > maxFilters {
		return ""
	}

	api.filters[f.id()] = f
	return f.id()
}

func (api *PullAPI) getBlocks(latestHeight uint64, filter *blocksFilter) ([]common.Hash, error) {
	nextHeight := filter.next()
	hashes := make([]common.Hash, 0)

	// todo we can optimize if needed by adding a getter method for range of blocks
	for i := nextHeight; i <= latestHeight; i++ {
		b, err := api.blocks.GetByHeight(i)
		if err != nil {
			return nil, err
		}
		h, err := b.Hash()
		if err != nil {
			continue
		}
		hashes = append(hashes, h)
	}

	return hashes, nil
}

func (api *PullAPI) getTransactions(latestHeight uint64, filter *transactionsFilter) (any, error) {
	txs := make([]models.Transaction, 0)
	hashes := make([]common.Hash, 0)

	// todo we can optimize if needed by adding a getter method for range of txs by heights
	for i := filter.next(); i <= latestHeight; i++ {
		b, err := api.blocks.GetByHeight(i)
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}

		// for now there will only be one tx per block
		for _, h := range b.TransactionHashes {
			tx, err := api.transactions.Get(h)
			if err != nil {
				return nil, err
			}

			h, err := tx.Hash()
			if err != nil {
				continue
			}
			txs = append(txs, tx)
			hashes = append(hashes, h)
		}
	}

	if filter.fullTx {
		return txs, nil
	}

	return hashes, nil
}

func (api *PullAPI) getLogs(latestHeight uint64, filter *logsFilter) (any, error) {
	nextHeight := filter.next()
	criteria := logs.FilterCriteria{
		Addresses: filter.criteria.Addresses,
		Topics:    filter.criteria.Topics,
	}

	to := filter.criteria.ToBlock
	// we use latest as default for end range
	end := big.NewInt(int64(latestHeight))
	// unless "to" is defined in the criteria
	if to != nil && to.Int64() >= 0 {
		end = to
		// if latest height is bigger than range "to" then we don't return anything
		if latestHeight > to.Uint64() {
			return nil, nil
		}
	}

	start := big.NewInt(int64(nextHeight))
	// we fetched all available data since start is now bigger than end value
	if start.Cmp(end) > 0 {
		return nil, nil
	}

	f, err := logs.NewRangeFilter(*start, *end, criteria, api.receipts)
	if err != nil {
		return nil, err
	}

	return f.Match()
}
