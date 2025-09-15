package models

import (
	"fmt"
	"sort"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/events"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

const (
	BlockExecutedQualifiedIdentifier        = string(events.EventTypeBlockExecuted)
	TransactionExecutedQualifiedIdentifier  = string(events.EventTypeTransactionExecuted)
	FeeParametersChangedQualifiedIdentifier = "FlowFees.FeeParametersChanged"
)

// isBlockExecutedEvent checks whether the given event contains block executed data.
func isBlockExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return event.EventType.QualifiedIdentifier == BlockExecutedQualifiedIdentifier
}

// isTransactionExecutedEvent checks whether the given event contains transaction executed data.
func isTransactionExecutedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return event.EventType.QualifiedIdentifier == TransactionExecutedQualifiedIdentifier
}

// isFeeParametersChangedEvent checks whether the given event contains updates
// to Flow fees parameters.
func isFeeParametersChangedEvent(event cadence.Event) bool {
	if event.EventType == nil {
		return false
	}
	return event.EventType.QualifiedIdentifier == FeeParametersChangedQualifiedIdentifier
}

// CadenceEvents contains Flow emitted events containing one or zero evm block executed event,
// and multiple or zero evm transaction events.
type CadenceEvents struct {
	events            flow.BlockEvents                 // Flow events for a specific flow block
	block             *Block                           // EVM block (at most one per Flow block)
	blockEventPayload *events.BlockEventPayload        // EVM.BlockExecuted event payload (at most one per Flow block)
	transactions      []Transaction                    // transactions in the EVM block
	txEventPayloads   []events.TransactionEventPayload // EVM.TransactionExecuted event payloads
	receipts          []*Receipt                       // receipts for transactions
	feeParameters     *FeeParameters                   // updates to Flow fees parameters
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

	// Log index field holds the index position in the entire block
	logIndex := uint(0)
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
			l.BlockNumber = rcp.BlockNumber.Uint64()
			l.BlockHash = rcp.BlockHash
			l.TxHash = rcp.TxHash
			l.TxIndex = rcp.TransactionIndex
			l.Index = logIndex
			l.Removed = false
			logIndex++
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
				return nil, fmt.Errorf("EVM block was already set for Flow block: %d", events.Height)
			}

			block, blockEventPayload, err := decodeBlockEvent(val)
			if err != nil {
				return nil, err
			}

			e.block = block
			e.blockEventPayload = blockEventPayload
			continue
		}

		if isTransactionExecutedEvent(val) {
			tx, receipt, txEventPayload, err := decodeTransactionEvent(val)
			if err != nil {
				return nil, err
			}

			e.transactions = append(e.transactions, tx)
			e.txEventPayloads = append(e.txEventPayloads, *txEventPayload)
			e.receipts = append(e.receipts, receipt)
		}

		if isFeeParametersChangedEvent(val) {
			feeParameters, err := decodeFeeParametersChangedEvent(val)
			if err != nil {
				return nil, err
			}

			e.feeParameters = feeParameters
		}
	}

	// safety check, we have a missing block in the events
	if e.block == nil && len(e.transactions) > 0 {
		return nil, fmt.Errorf(
			"%w EVM block nil at flow block: %d",
			errs.ErrMissingBlock,
			events.Height,
		)
	}

	if e.block != nil {
		txHashes := evmTypes.TransactionHashes{}
		for _, tx := range e.transactions {
			txHashes = append(txHashes, tx.Hash())
		}
		if e.block.TransactionHashRoot != txHashes.RootHash() {
			return nil, fmt.Errorf(
				"%w EVM block %d references missing transaction/s",
				errs.ErrMissingTransactions,
				e.block.Height,
			)
		}
	}

	return e, nil
}

// Block evm block. If event doesn't contain EVM block the return value is nil.
func (c *CadenceEvents) Block() *Block {
	return c.block
}

// BlockEventPayload returns the EVM.BlockExecuted event payload. If the Flow block
// events do not contain an EVM block, the return value is nil.
func (c *CadenceEvents) BlockEventPayload() *events.BlockEventPayload {
	return c.blockEventPayload
}

// Transactions included in the EVM block, if event doesn't
// contain EVM transactions the return value is nil.
func (c *CadenceEvents) Transactions() []Transaction {
	return c.transactions
}

// TxEventPayloads returns the EVM.TransactionExecuted event payloads for the
// current EVM block. If the Flow block events do not contain any EVM transactions
// the return value is nil.
func (c *CadenceEvents) TxEventPayloads() []events.TransactionEventPayload {
	return c.txEventPayloads
}

// Receipts included in the EVM block, if event doesn't
// contain EVM transactions the return value is nil.
func (c *CadenceEvents) Receipts() []*Receipt {
	return c.receipts
}

// FeeParameters returns any updates to the Flow fees parameters.
func (c *CadenceEvents) FeeParameters() *FeeParameters {
	return c.feeParameters
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

// NewMultiBlockEvents will decode any possible `EVM.TransactionExecuted` &
// `EVM.BlockExecuted` events and populate the resulting `Block`, `Transaction` &
// `Receipt` values.
// The `EVM.TransactionExecuted` events are expected to be properly sorted by
// the caller.
// Use this method when dealing with `flow.BlockEvents` from multiple Flow blocks.
// The `EVM.TransactionExecuted` events could be produced at a Flow block, that
// comes prior to the Flow block that produced the `EVM.BlockExecuted` event.
func NewMultiBlockEvents(events flow.BlockEvents) BlockEvents {
	cdcEvents, err := NewCadenceEvents(events)
	return BlockEvents{
		Events: cdcEvents,
		Err:    err,
	}
}

// NewSingleBlockEvents will decode any possible `EVM.TransactionExecuted` &
// `EVM.BlockExecuted` events and populate the resulting `Block`, `Transaction` &
// `Receipt` values.
// The `EVM.TransactionExecuted` events will be sorted by `TransactionIndex` &
// `EventIndex`, prior to decoding.
// Use this method when dealing with `flow.BlockEvents` from a single Flow block.
func NewSingleBlockEvents(events flow.BlockEvents) BlockEvents {
	// first we sort all the events in the block, by their TransactionIndex,
	// and then we also sort events in the same transaction, by their EventIndex.
	sort.Slice(events.Events, func(i, j int) bool {
		if events.Events[i].TransactionIndex != events.Events[j].TransactionIndex {
			return events.Events[i].TransactionIndex < events.Events[j].TransactionIndex
		}
		return events.Events[i].EventIndex < events.Events[j].EventIndex
	})

	cdcEvents, err := NewCadenceEvents(events)
	return BlockEvents{
		Events: cdcEvents,
		Err:    err,
	}
}

func NewBlockEventsError(err error) BlockEvents {
	return BlockEvents{
		Err: err,
	}
}
