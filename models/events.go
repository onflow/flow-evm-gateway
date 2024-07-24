package models

import (
	"fmt"
	"strings"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/events"
)

// isBlockExecutedEvent checks whether the given event contains block executed data.
func isBlockExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return strings.Contains(event.EventType.ID(), string(events.EventTypeBlockExecuted))
}

// isTransactionExecutedEvent checks whether the given event contains transaction executed data.
func isTransactionExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return strings.Contains(event.EventType.ID(), string(events.EventTypeTransactionExecuted))
}

// CadenceEvents contains Flow emitted events containing one or zero evm block executed event,
// and multiple or zero evm transaction events.
type CadenceEvents struct {
	events       flow.BlockEvents  // Flow events for a specific flow block
	block        *Block            // EVM block (at most one per Flow block)
	transactions []Transaction     // transactions in the EVM block
	receipts     []*StorageReceipt // receipts for transactions
}

// NewCadenceEvents decodes the events into evm types.
func NewCadenceEvents(events flow.BlockEvents) (*CadenceEvents, error) {
	e, err := decodeCadenceEvents(events)
	if err != nil {
		return nil, err
	}

	// if cadence event is empty don't calculate any dynamic values
	if e.Empty() {
		return e, nil
	}

	// calculate dynamic values
	cumulativeGasUsed := uint64(0)
	blockHash, err := e.block.Hash()
	if err != nil {
		return nil, err
	}

	for i, rcp := range e.receipts {
		// add transaction hashes to the block
		e.block.TransactionHashes = append(e.block.TransactionHashes, rcp.TxHash)
		// calculate cumulative gas used up to that point
		cumulativeGasUsed += rcp.GasUsed
		rcp.CumulativeGasUsed = cumulativeGasUsed
		// set the transaction index
		rcp.TransactionIndex = uint(i)
		// set calculate block hash
		rcp.BlockHash = blockHash
		// dynamically add missing log fields
		for _, l := range rcp.Logs {
			l.TxHash = rcp.TxHash
			l.BlockNumber = rcp.BlockNumber.Uint64()
			l.Index = rcp.TransactionIndex
		}
	}

	return e, nil
}

// decodeCadenceEvents accepts Flow Cadence event and decodes it into CadenceEvents
// collection. It also performs safety checks on the provided event data.
func decodeCadenceEvents(events flow.BlockEvents) (*CadenceEvents, error) {
	e := &CadenceEvents{events: events}

	// decode and cache block and transactions
	for _, event := range events.Events {
		val := event.Value

		if isBlockExecutedEvent(val) {
			if e.block != nil { // safety check, we can only have 1 or 0 evm blocks per one flow block
				return nil, fmt.Errorf("EVM block was already set for this Flow block, invalid event data")
			}

			block, err := decodeBlockEvent(val)
			if err != nil {
				return nil, err
			}

			e.block = block
			continue
		}

		if isTransactionExecutedEvent(val) {
			tx, receipt, err := decodeTransactionEvent(val)
			if err != nil {
				return nil, err
			}

			e.transactions = append(e.transactions, tx)
			e.receipts = append(e.receipts, receipt)
		}
	}

	// safety check, we can't have an empty block with transactions
	if e.block == nil && len(e.transactions) > 0 {
		return nil, fmt.Errorf("EVM block can not be nil if transactions are present, invalid event data")
	}

	return e, nil
}

// Block evm block. If event doesn't contain EVM block the return value is nil.
func (c *CadenceEvents) Block() *Block {
	return c.block
}

// Transactions included in the EVM block, if event doesn't
// contain EVM transactions the return value is nil.
func (c *CadenceEvents) Transactions() []Transaction {
	return c.transactions
}

// Receipts included in the EVM block, if event doesn't
// contain EVM transactions the return value is nil.
func (c *CadenceEvents) Receipts() []*StorageReceipt {
	return c.receipts
}

// Empty checks if there is an EVM block included in the events.
// If there are no evm block or transactions events this is a heartbeat event.
func (c *CadenceEvents) Empty() bool {
	return c.block == nil
}

// CadenceHeight returns the Flow Cadence height at which the events
// were emitted.
func (c *CadenceEvents) CadenceHeight() uint64 {
	return c.events.Height
}

// CadenceBlockID returns the Flow Cadence block ID.
func (c *CadenceEvents) CadenceBlockID() flow.Identifier {
	return c.events.BlockID
}

// Length of the Cadence events emitted.
func (c *CadenceEvents) Length() int {
	return len(c.events.Events)
}

// BlockEvents is a wrapper around events streamed, and it also contains an error
type BlockEvents struct {
	Events *CadenceEvents
	Err    error
}

func NewBlockEvents(events flow.BlockEvents) BlockEvents {
	blockEvents, err := NewCadenceEvents(events)

	return BlockEvents{
		Events: blockEvents,
		Err:    err,
	}
}

func NewBlockEventsError(err error) BlockEvents {
	return BlockEvents{
		Err: err,
	}
}
