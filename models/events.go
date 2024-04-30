package models

import (
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"golang.org/x/exp/slices"
	"strings"
)

// isBlockExecutedEvent checks whether the given event contains block executed data.
func isBlockExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return strings.Contains(event.EventType.ID(), string(types.EventTypeBlockExecuted))
}

// isTransactionExecutedEvent checks whether the given event contains transaction executed data.
func isTransactionExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return strings.Contains(event.EventType.ID(), string(types.EventTypeTransactionExecuted))
}

type CadenceEvents struct {
	events flow.BlockEvents
}

func NewCadenceEvents(events flow.BlockEvents) *CadenceEvents {
	return &CadenceEvents{events: events}
}

// Blocks finds the block evm events and decodes it into the blocks slice,
// if no block events are found nil slice is returned.
//
// Return values:
// blocks, nil - if blocks are found
// nil, nil - if no block are found
// nil, err - unexpected error
func (c *CadenceEvents) Blocks() ([]*types.Block, error) {
	blocks := make([]*types.Block, 0)
	for _, e := range c.events.Events {
		if isBlockExecutedEvent(e.Value) {
			block, err := decodeBlock(e.Value)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}

	// make sure order of heights is ordered, this is safety check
	slices.SortFunc(blocks, func(a, b *types.Block) int {
		return int(a.Height - b.Height)
	})

	return blocks, nil
}

// Transactions finds all the transactions evm events and decodes them into transaction slice,
// if no transactions is found nil is returned.
//
// Return values:
// []transaction, nil - if transactions are found
// nil, nil - if no transactions are found
// nil, err - unexpected error
func (c *CadenceEvents) Transactions() ([]Transaction, []*gethTypes.Receipt, error) {
	txs := make([]Transaction, 0)
	rcps := make([]*gethTypes.Receipt, 0)
	for _, e := range c.events.Events {
		if isTransactionExecutedEvent(e.Value) {
			tx, err := decodeTransaction(e.Value)
			if err != nil {
				return nil, nil, err
			}
			rcp, err := decodeReceipt(e.Value)
			if err != nil {
				return nil, nil, err
			}

			txs = append(txs, tx)
			rcps = append(rcps, rcp)
		}
	}

	return txs, rcps, nil
}

// Empty checks if there are any evm block or transactions events.
// If there are no evm block or transactions events this is a heartbeat
// event that is broadcast in intervals.
func (c *CadenceEvents) Empty() bool {
	b, _ := c.Blocks()
	return len(b) == 0
}

// CadenceHeight returns the Flow Cadence height at which the events
// were emitted.
func (c *CadenceEvents) CadenceHeight() uint64 {
	return c.events.Height
}

// Length of the Cadence events emitted.
func (c *CadenceEvents) Length() int {
	return len(c.events.Events)
}
