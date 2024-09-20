package requester

import (
	"context"

	"github.com/onflow/atree"
	"github.com/onflow/flow-go/engine/common/rpc/convert"
	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow/protobuf/go/flow/entities"
	"github.com/onflow/flow/protobuf/go/flow/executiondata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ atree.Ledger = &remoteLedger{}

func newRemoteLedger(
	client executiondata.ExecutionDataAPIClient,
	cadenceHeight uint64,
) (*remoteLedger, error) {
	return &remoteLedger{
		execution: client,
		height:    cadenceHeight,
	}, nil
}

// remoteLedger is a ledger that uses execution data APIs to fetch register values,
// thus simulating execution against the host network.
//
// The ledger implements atree.Ledger interface which is used by the type.stateDB
// to inspect the state.
type remoteLedger struct {
	execution executiondata.ExecutionDataAPIClient
	height    uint64
}

func (l *remoteLedger) GetValue(owner, key []byte) ([]byte, error) {
	id := flow.RegisterID{
		Key:   string(key),
		Owner: string(owner),
	}
	registerID := convert.RegisterIDToMessage(id)

	response, err := l.execution.GetRegisterValues(
		context.Background(),
		&executiondata.GetRegisterValuesRequest{
			BlockHeight: l.height,
			RegisterIds: []*entities.RegisterID{registerID},
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

func (l *remoteLedger) ValueExists(owner, key []byte) (exists bool, err error) {
	val, err := l.GetValue(owner, key)
	return val != nil, err
}

func (l *remoteLedger) SetValue(owner, key, value []byte) (err error) {
	panic("read only")
}

func (l *remoteLedger) AllocateSlabIndex(owner []byte) (atree.SlabIndex, error) {
	panic("read only")
}
