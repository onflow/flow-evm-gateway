package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/filters"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/zerolog"
)

// maxFilters limits the max active filters at any time to prevent abuse and OOM
const maxFilters = 10_000

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
		nextHeight: currentHeight + 1, // we are only interested in next heights not current
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
var _ filter = &logsFilter{}

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

func newTransactionsFilter(expiry time.Duration, latestHeight uint64, fullTx bool) *transactionsFilter {
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
	mux          sync.Mutex
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

	f := newTransactionsFilter(api.config.FilterExpiry, last, full)

	api.logger.Debug().
		Str("id", string(f.id())).
		Uint64("height", last).
		Bool("full-tx", full).
		Msg("new pending transaction filter")

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

	api.logger.Debug().
		Str("id", string(f.id())).
		Uint64("height", last).
		Msg("new blocks filter")

	return api.addFilter(f), nil
}

func (api *PullAPI) UninstallFilter(id rpc.ID) bool {
	api.mux.Lock()
	defer api.mux.Unlock()

	if _, ok := api.filters[id]; !ok { // not found
		return false
	}

	delete(api.filters, id)

	api.logger.Debug().Str("id", string(id)).Msg("uninstalling filter")
	return true
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

	api.logger.Debug().
		Str("id", string(f.id())).
		Uint64("height", latest).
		Any("from", criteria.FromBlock).
		Any("to", criteria.ToBlock).
		Msg("new logs filter")

	return api.addFilter(f), nil
}

// GetFilterLogs returns the logs for the filter with the given id.
// If the filter could not be found, `nil` is returned.
func (api *PullAPI) GetFilterLogs(
	ctx context.Context,
	id rpc.ID,
) ([]*gethTypes.Log, error) {
	api.mux.Lock()
	defer api.mux.Unlock()

	filter, ok := api.filters[id]
	if !ok {
		return nil, errors.Join(
			errs.ErrNotFound,
			fmt.Errorf("filted by id %s does not exist", id),
		)
	}

	if filter.expired() {
		api.UninstallFilter(id)
		return nil, errors.Join(
			errs.ErrNotFound,
			fmt.Errorf("filted by id %s has expired", id),
		)
	}

	logsFilter, ok := filter.(*logsFilter)
	if !ok {
		return nil, fmt.Errorf("filted by id %s is not a logs filter", id)
	}

	current, err := api.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	result, err := api.getLogs(current, logsFilter)
	if err != nil {
		return nil, err
	}

	logs, ok := result.([]*gethTypes.Log)
	if !ok {
		return nil, fmt.Errorf("logs filter returned incorrect type: %T", logs)
	}

	return logs, nil
}

// GetFilterChanges returns the logs for the filter with the given id since
// last time it was called. This can be used for polling.
//
// For pending transaction and block filters the result is []common.Hash.
// (pending)Log filters return []Log.
func (api *PullAPI) GetFilterChanges(id rpc.ID) (any, error) {
	api.mux.Lock()
	defer api.mux.Unlock()

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

	var result any
	switch filterType := f.(type) {
	case *blocksFilter:
		result, err = api.getBlocks(current, filterType)
	case *transactionsFilter:
		result, err = api.getTransactions(current, filterType)
	case *logsFilter:
		result, err = api.getLogs(current, filterType)
	default:
		return nil, fmt.Errorf("invalid filter type")
	}
	if err != nil {
		return nil, err
	}

	f.updateUsed(current)
	return result, nil
}

// filterExpiryChecker continuously monitors all the registered filters and
// if any of them has been idle for too long i.e. no requests have been made
// for a period defined as filterExpiry using the filter ID it will be removed.
func (api *PullAPI) filterExpiryChecker() {
	for range time.Tick(api.config.FilterExpiry) {
		for id, f := range api.filters {
			if f.expired() {
				api.logger.Debug().Str("id", string(id)).Msg("filter expired")
				api.UninstallFilter(id)
			}
		}
	}
}

func (api *PullAPI) addFilter(f filter) rpc.ID {
	api.mux.Lock()
	defer api.mux.Unlock()

	if len(api.filters) > maxFilters {
		return ""
	}

	api.filters[f.id()] = f
	return f.id()
}

func (api *PullAPI) getBlocks(latestHeight uint64, filter *blocksFilter) ([]common.Hash, error) {
	nextHeight := filter.next()
	hashes := make([]common.Hash, 0)

	api.logger.Debug().
		Uint64("latest", latestHeight).
		Uint64("from", nextHeight).
		Uint64("to", latestHeight).
		Msg("get filter blocks")

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
	nextHeight := filter.next()

	api.logger.Debug().
		Uint64("latest", latestHeight).
		Uint64("from", nextHeight).
		Uint64("to", latestHeight).
		Msg("get filter transactions")

	// todo we can optimize if needed by adding a getter method for range of txs by heights
	for i := nextHeight; i <= latestHeight; i++ {
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
			return []*gethTypes.Log{}, nil
		}
	}

	start := big.NewInt(int64(nextHeight))
	// we fetched all available data since start is now bigger than end value
	if start.Cmp(end) > 0 {
		return []*gethTypes.Log{}, nil
	}

	f, err := logs.NewRangeFilter(*start, *end, criteria, api.receipts)
	if err != nil {
		return nil, fmt.Errorf(
			"could not create range filter from %d to %d: %w",
			start.Int64(),
			end.Int64(),
			err,
		)
	}

	api.logger.Debug().
		Uint64("latest", latestHeight).
		Int64("from", start.Int64()).
		Int64("to", end.Int64()).
		Msg("get filter logs")

	return f.Match()
}
