package logs

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	"math/big"
	"slices"
)

// FilterCriteria for log filtering.
// Address of the contract emitting the log.
// Topics that match the log topics, following the format:
// [] “anything”
// [A] “A in first position (and anything after)”
// [null, B] “anything in first position AND B in second position (and anything after)”
// [A, B] “A in first position AND B in second position (and anything after)”
// [[A, B], [A, B]] “(A OR B) in first position AND (A OR B) in second position (and anything after)”
type FilterCriteria struct {
	Addresses []common.Address
	Topics    [][]common.Hash
}

// todo think about creating an interface for all the filters
// Filter interface { Match() (chan *gethTypes.Log, error) }

// RangeFilter matches all the indexed logs within the range defined as
// start and end block height. The start must be strictly smaller than end value.
type RangeFilter struct {
	start, end *big.Int
	criteria   FilterCriteria
	receipts   storage.ReceiptIndexer
}

func NewRangeFilter(
	start, end big.Int,
	criteria FilterCriteria,
	receipts storage.ReceiptIndexer,
) *RangeFilter {
	return &RangeFilter{
		start:    &start,
		end:      &end,
		criteria: criteria,
		receipts: receipts,
	}
}

func (r *RangeFilter) Match() ([]*gethTypes.Log, error) {
	if r.start.Cmp(r.end) != -1 {
		return nil, fmt.Errorf("invalid start and end block height, start must be smaller than end value")
	}

	blooms, heights, err := r.receipts.BloomsForBlockRange(r.start, r.end)
	if err != nil {
		return nil, err
	}

	if len(blooms) != len(heights) {
		return nil, fmt.Errorf("bloom values don't match height values") // this should never happen
	}

	logs := make([]*gethTypes.Log, 0)
	for i, bloom := range blooms {
		if !bloomMatch(*bloom, r.criteria) {
			continue
		}

		// todo do this concurrently
		receipt, err := r.receipts.GetByBlockHeight(heights[i])
		if err != nil {
			return nil, err
		}

		for _, log := range receipt.Logs {
			if exactMatch(log, r.criteria) {
				logs = append(logs, log)
			}
		}
	}

	return logs, nil
}

// IDFilter matches all logs against the criteria found in a single block identified
// by the provided block ID.
type IDFilter struct {
	id       common.Hash
	criteria FilterCriteria
	blocks   storage.BlockIndexer
	receipts storage.ReceiptIndexer
}

func NewIDFilter(
	id common.Hash,
	criteria FilterCriteria,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
) *IDFilter {
	return &IDFilter{
		id:       id,
		criteria: criteria,
		blocks:   blocks,
		receipts: receipts,
	}
}

func (i *IDFilter) Match() ([]*gethTypes.Log, error) {
	blk, err := i.blocks.GetByID(i.id)
	if err != nil {
		return nil, err
	}

	receipt, err := i.receipts.GetByBlockHeight(big.NewInt(int64(blk.Height)))
	if err != nil {
		return nil, err
	}

	logs := make([]*gethTypes.Log, 0)
	for _, log := range receipt.Logs {
		if exactMatch(log, i.criteria) {
			logs = append(logs, log)
		}
	}

	return logs, nil
}

// StreamFilter matches all the logs against the criteria from the receipt channel.
type StreamFilter struct {
	criteria      FilterCriteria
	receiptStream chan *gethTypes.Receipt
}

func NewStreamFilter(criteria FilterCriteria, receipts chan *gethTypes.Receipt) *StreamFilter {
	return &StreamFilter{
		criteria:      criteria,
		receiptStream: receipts,
	}
}

func (s *StreamFilter) Match() (chan *gethTypes.Log, error) {
	logs := make(chan *gethTypes.Log)

	go func() {
		defer close(logs)

		for {
			select {
			case receipt, ok := <-s.receiptStream:
				if !ok {
					return // exit the goroutine if receiptStream is closed
				}

				if !bloomMatch(receipt.Bloom, s.criteria) {
					continue
				}

				for _, log := range receipt.Logs {
					if exactMatch(log, s.criteria) {
						logs <- log
					}
				}
			}
		}
	}()

	return logs, nil
}

// exactMatch checks the topic and address values of the log match the filer exactly.
func exactMatch(log *gethTypes.Log, criteria FilterCriteria) bool {
	if len(criteria.Topics) > len(log.Topics) {
		return false
	}

	for _, sub := range criteria.Topics {
		for _, topic := range sub {
			if !slices.Contains(log.Topics, topic) {
				return false
			}
		}
	}

	if !slices.Contains(criteria.Addresses, log.Address) {
		return false
	}

	return true
}

// bloomMatch takes a bloom value and tests if the addresses and topics provided pass the bloom filter.
// This acts as a fast probabilistic test that might produce false-positives but not false-negatives.
// If true is returned we should further check against the exactMatch to really make sure the log is matched.
//
// source: https://github.com/ethereum/go-ethereum/blob/8d1db1601d3a9e4fd067558a49db6f0b879c9b48/eth/filters/filter.go#L395
func bloomMatch(bloom gethTypes.Bloom, criteria FilterCriteria) bool {
	if len(criteria.Addresses) > 0 {
		var included bool
		for _, addr := range criteria.Addresses {
			if gethTypes.BloomLookup(bloom, addr) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}

	for _, sub := range criteria.Topics {
		included := len(sub) == 0 // empty rule set == wildcard
		for _, topic := range sub {
			if gethTypes.BloomLookup(bloom, topic) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}
	return true
}
