package evm

import (
	"context"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/engine/common/rpc/convert"
	"github.com/onflow/flow-go/fvm/environment"
	"github.com/onflow/flow-go/fvm/errors"
	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow/protobuf/go/flow/entities"
	"github.com/onflow/flow/protobuf/go/flow/executiondata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ atree.Ledger = &remoteLedger{}

// todo analyse and possibly add register cache for specific cadence height
// this could resolve locally a register that would otherwise need to be cached again
// A suitable cache for this would be golang-lru/v2 two queue cache (2q)

// remoteLedger is a ledger that uses execution data APIs to fetch register values,
// thus simulating execution against the host network.
//
// The ledger implements atree.Ledger interface which is used by the type.stateDB
// to inspect the state.
type remoteLedger struct {
	execution executiondata.ExecutionDataAPIClient
	height    uint64
	diffs     map[string][]byte
	slabs     map[string]atree.SlabIndex
}

func NewRemoteLedger(
	client executiondata.ExecutionDataAPIClient,
	cadenceHeight uint64,
) (*remoteLedger, error) {
	return &remoteLedger{
		execution: client,
		height:    cadenceHeight,
		diffs:     make(map[string][]byte),
		slabs:     make(map[string]atree.SlabIndex),
	}, nil
}

func (l *remoteLedger) GetValue(owner, key []byte) ([]byte, error) {
	id := flow.RegisterID{
		Key:   string(key),
		Owner: string(owner),
	}

	// first check local changes
	if val, ok := l.diffs[id.String()]; ok {
		return val, nil
	}

	register, err := l.getRegister(id)
	if err != nil {
		return nil, err
	}

	// also store it locally to cache
	l.diffs[id.String()] = register

	return register, nil
}

func (l *remoteLedger) ValueExists(owner, key []byte) (exists bool, err error) {
	val, err := l.GetValue(owner, key)
	return val != nil, err
}

func (l *remoteLedger) SetValue(owner, key, value []byte) (err error) {
	id := flow.RegisterID{
		Key:   string(key),
		Owner: string(owner),
	}

	l.diffs[id.String()] = value
	return nil
}

func (l *remoteLedger) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	address := flow.BytesToAddress(owner)
	id := flow.AccountStatusRegisterID(address)

	if val, ok := l.slabs[id.String()]; ok {
		return val, nil
	}

	val, err := l.getRegister(id)
	if err != nil {
		return atree.SlabIndex{}, err
	}

	if len(val) == 0 {
		return atree.SlabIndex{}, errors.NewAccountNotFoundError(address)
	}

	account, err := environment.AccountStatusFromBytes(val)
	if err != nil {
		return atree.SlabIndex{}, err
	}

	slab := account.SlabIndex()
	l.slabs[id.String()] = slab

	return slab, nil
}

func (l *remoteLedger) getRegister(registerID flow.RegisterID) ([]byte, error) {
	id := convert.RegisterIDToMessage(registerID)

	response, err := l.execution.GetRegisterValues(
		context.Background(),
		&executiondata.GetRegisterValuesRequest{
			BlockHeight: l.height,
			RegisterIds: []*entities.RegisterID{id},
		},
	)
	errorCode := status.Code(err)
	if err != nil && errorCode != codes.NotFound && errorCode != codes.OutOfRange {
		return nil, err
	}

	if response != nil && len(response.Values) > 0 {
		// we only request one register so 0 index
		return response.Values[0], nil
	}

	return nil, nil
}
