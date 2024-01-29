package api

import (
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/access/grpc"
)

func NewFlowClient(host string) (access.Client, error) {
	client, err := grpc.NewClient(host)
	if err != nil {
		return nil, err
	}

	return client, nil
}
