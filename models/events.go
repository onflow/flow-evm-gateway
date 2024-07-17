package models

import (
	"github.com/onflow/cadence"
	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	flowGo "github.com/onflow/flow-go/model/flow"
	"golang.org/x/exp/slices"
)

// isBlockExecutedEvent checks whether the given event contains block executed data.
func isBlockExecutedEvent(
	event cadence.Event,
	evmBlockExecutedEventID string,
) bool {
	if event.EventType == nil {
		return false
	}
	return event.EventType.ID() == evmBlockExecutedEventID
}

// isTransactionExecutedEvent checks whether the given event contains transaction executed data.
func isTransactionExecutedEvent(
	event cadence.Event,
	evmTxExecutedEventID string,
) bool {
	if event.EventType == nil {
		return false
	}
	return event.EventType.ID() == evmTxExecutedEventID
}

type CadenceEvents struct {
	events      flow.BlockEvents
	evmEventIDs EVMEventIdentifiers
}

func NewCadenceEvents(
	events flow.BlockEvents,
	evmEventIDs EVMEventIdentifiers,
) *CadenceEvents {
	return &CadenceEvents{
		events:      events,
		evmEventIDs: evmEventIDs,
	}
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
		if isBlockExecutedEvent(e.Value, c.evmEventIDs.BlockExecutedEventID) {
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
func (c *CadenceEvents) Transactions() ([]Transaction, []*StorageReceipt, error) {
	txs := make([]Transaction, 0)
	rcps := make([]*StorageReceipt, 0)
	for _, e := range c.events.Events {
		if isTransactionExecutedEvent(e.Value, c.evmEventIDs.TransactionExecutedEventID) {
			tx, receipt, err := decodeTransactionEvent(e.Value)
			if err != nil {
				return nil, nil, err
			}

			txs = append(txs, tx)
			rcps = append(rcps, receipt)
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

func NewBlockEvents(
	events flow.BlockEvents,
	evmEventIDs EVMEventIdentifiers,
) BlockEvents {
	return BlockEvents{
		Events: NewCadenceEvents(events, evmEventIDs),
	}
}

func NewBlockEventsError(err error) BlockEvents {
	return BlockEvents{
		Err: err,
	}
}

type EVMEventIdentifiers struct {
	BlockExecutedEventID       string
	TransactionExecutedEventID string
}

func NewEVMEventIdentifiers(chainID flowGo.ChainID) EVMEventIdentifiers {
	evmAddress := common.Address(
		systemcontracts.SystemContractsForChain(chainID).EVMContract.Address,
	)

	blockExecutedEventID := common.NewAddressLocation(
		nil,
		evmAddress,
		string(types.EventTypeBlockExecuted),
	).ID()

	transactionExecutedEventID := common.NewAddressLocation(
		nil,
		evmAddress,
		string(types.EventTypeTransactionExecuted),
	).ID()

	return EVMEventIdentifiers{
		BlockExecutedEventID:       blockExecutedEventID,
		TransactionExecutedEventID: transactionExecutedEventID,
	}
}

var DefaultEVMEventIdentifiers = NewEVMEventIdentifiers(flowGo.Previewnet)
