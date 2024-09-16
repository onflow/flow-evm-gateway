package requester

import (
	"bytes"
	"context"
	"fmt"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/engine/common/rpc/convert"
	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow/protobuf/go/flow/entities"
	"github.com/onflow/flow/protobuf/go/flow/executiondata"
	"golang.org/x/exp/maps"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

// todo we should introduce a new state of the block, indexed, executed and validate
// after this validator does the validation

var _ atree.Ledger = &RegisterValidator{}

// RegisterValidator keeps track of all set register during execution and is checked
// once the block is executed.
type RegisterValidator struct {
	*pebble.Register
	execution executiondata.ExecutionDataAPIClient
	updates   map[flow.RegisterID][]byte
}

func NewRegisterValidator(
	register *pebble.Register,
	execution executiondata.ExecutionDataAPIClient,
) *RegisterValidator {
	return &RegisterValidator{
		Register:  register,
		execution: execution,
		updates:   make(map[flow.RegisterID][]byte),
	}
}

func (r *RegisterValidator) SetValue(owner, key, value []byte) (err error) {
	id := flow.RegisterID{
		Key:   string(key),
		Owner: string(owner),
	}
	r.updates[id] = value

	return r.Register.SetValue(owner, key, value)
}

// ValidateBlock will go over all registers that were set during block execution and compare
// them against the registers stored on-chain using an execution data client.
// Expected errors:
// - Invalid error if there is a mismatch in any of the register values
// Any other error is an issue with client request or response.
func (r *RegisterValidator) ValidateBlock(height uint64) error {
	registers := make([]*entities.RegisterID, 0)
	values := maps.Values(r.updates)

	for id := range r.updates {
		registers = append(registers, convert.RegisterIDToMessage(id))
	}

	response, err := r.execution.GetRegisterValues(
		context.Background(),
		&executiondata.GetRegisterValuesRequest{
			BlockHeight: height,
			RegisterIds: registers,
		},
	)
	if err != nil {
		return fmt.Errorf("invalid request for register values: %w", err)
	}

	for i, val := range response.Values {
		if !bytes.Equal(values[i], val) {
			return fmt.Errorf(
				"%w register %s with value %x does not match remote state value %x at height %d",
				errs.ErrInvalid,
				maps.Keys(r.updates)[i].String(),
				values[i],
				val,
				height,
			)
		}
	}

	return nil
}
