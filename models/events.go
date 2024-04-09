package models

import (
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
)

var evmLocation common.Location = flow.EVMLocation{}

var BlockExecutedEventType = evmLocation.TypeID(
	nil,
	string(types.EventTypeBlockExecuted),
)

var TransactionExecutedEventType = evmLocation.TypeID(
	nil,
	string(types.EventTypeTransactionExecuted),
)

// isBlockExecutedEvent checks whether the given event contains block executed data.
func isBlockExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return common.TypeID(event.EventType.ID()) == BlockExecutedEventType
}

// isTransactionExecutedEvent checks whether the given event contains transaction executed data.
func isTransactionExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return common.TypeID(event.EventType.ID()) == TransactionExecutedEventType
}

type CadenceEvents struct {
	events flow.BlockEvents
}

func NewCadenceEvents(events flow.BlockEvents) *CadenceEvents {
	return &CadenceEvents{events: events}
}

// Block finds the block evm event and decodes it into the block type,
// if no block event is found nil is returned.
//
// Return values:
// block, nil - if block is found
// nil, nil - if no block is found
// nil, err - unexpected error
func (c *CadenceEvents) Block() (*types.Block, error) {
	for _, e := range c.events.Events {
		if isBlockExecutedEvent(e.Value) {
			return decodeBlock(e.Value)
		}
	}
	return nil, nil
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
	b, _ := c.Block()
	return b == nil
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
