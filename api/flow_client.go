package api

import (
	"context"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk/access/grpc"
)

type FlowAccessAPI interface {
	ExecuteScriptAtLatestBlock(
		ctx context.Context,
		script []byte,
		arguments []cadence.Value,
	) (cadence.Value, error)
}

type FlowClient struct {
	client *grpc.Client
}

var _ FlowAccessAPI = &FlowClient{}

func (fc *FlowClient) ExecuteScriptAtLatestBlock(
	ctx context.Context,
	script []byte,
	arguments []cadence.Value,
) (cadence.Value, error) {
	return fc.client.ExecuteScriptAtLatestBlock(ctx, script, arguments)
}

func NewFlowClient(host string) (FlowAccessAPI, error) {
	client, err := grpc.NewClient(host)
	if err != nil {
		return nil, err
	}

	return &FlowClient{client: client}, nil
}

type MockFlowClient struct {
	ExecuteScriptAtLatestBlockFunc func(context.Context, []byte, []cadence.Value) (cadence.Value, error)
}

var _ FlowAccessAPI = &MockFlowClient{}

func (m MockFlowClient) ExecuteScriptAtLatestBlock(
	ctx context.Context,
	script []byte,
	arguments []cadence.Value,
) (cadence.Value, error) {
	if m.ExecuteScriptAtLatestBlockFunc == nil {
		panic("'ExecuteScriptAtLatestBlock' is not implemented")
	}

	return m.ExecuteScriptAtLatestBlockFunc(ctx, script, arguments)
}
