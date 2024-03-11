package models

import (
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

// IsBlockExecutedEvent checks whether the given event contains block executed data.
func IsBlockExecutedEvent(event cadence.Event) bool {
	return common.TypeID(event.EventType.ID()) == BlockExecutedEventType
}

// IsTransactionExecutedEvent checks whether the given event contains transaction executed data.
func IsTransactionExecutedEvent(event cadence.Event) bool {
	return common.TypeID(event.EventType.ID()) == TransactionExecutedEventType
}
