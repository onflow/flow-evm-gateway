package logs

import (
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"golang.org/x/exp/slices"

	"github.com/onflow/flow-evm-gateway/storage"
)

// The maximum number of topic criteria allowed
const maxTopics = 4

// The maximum number of addresses allowed
const maxAddresses = 6

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

func NewFilterCriteria(addresses []common.Address, topics [][]common.Hash) (*FilterCriteria, error) {
	if len(topics) > maxTopics {
		return nil, fmt.Errorf("max topics exceeded, only %d allowed", maxTopics)
	}
	if len(addresses) > maxAddresses {
		return nil, fmt.Errorf("max addresses exceeded, only %d allowed", maxAddresses)
	}

	return &FilterCriteria{
		Addresses: addresses,
		Topics:    topics,
	}, nil
}

// RangeFilter matches all the indexed logs within the range defined as
// start and end block height. The start must be strictly smaller or equal than end value.
type RangeFilter struct {
	start, end *big.Int
	criteria   FilterCriteria
	receipts   storage.ReceiptIndexer
}

func NewRangeFilter(
	start, end big.Int,
	criteria FilterCriteria,
	receipts storage.ReceiptIndexer,
) (*RangeFilter, error) {
	if start.Cmp(&end) > 0 {
		return nil, fmt.Errorf("invalid start and end block height, start must be smaller or equal than end value")
	}

	return &RangeFilter{
		start:    &start,
		end:      &end,
		criteria: criteria,
		receipts: receipts,
	}, nil
}

func (r *RangeFilter) Match() ([]*gethTypes.Log, error) {
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
		receipts, err := r.receipts.GetByBlockHeight(heights[i])
		if err != nil {
			return nil, err
		}

		for _, receipt := range receipts {
			for _, log := range receipt.Logs {
				if exactMatch(log, r.criteria) {
					logs = append(logs, log)
				}
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

	receipts, err := i.receipts.GetByBlockHeight(big.NewInt(int64(blk.Height)))
	if err != nil {
		return nil, err
	}

	logs := make([]*gethTypes.Log, 0)
	for _, receipt := range receipts {
		for _, log := range receipt.Logs {
			if exactMatch(log, i.criteria) {
				logs = append(logs, log)
			}
		}
	}

	return logs, nil
}

// exactMatch checks the topic and address values of the log match the filter exactly.
func exactMatch(log *gethTypes.Log, criteria FilterCriteria) bool {
	// todo support no address matching all

	// check criteria doesn't have more topics than the log, but it can have less due to wildcards
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

	return slices.Contains(criteria.Addresses, log.Address)
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
