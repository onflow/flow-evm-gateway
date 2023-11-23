package indexer

import (
	"context"
	"fmt"

	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow/protobuf/go/flow/access"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func GetChain(ctx context.Context, accessURL string) (flow.Chain, error) {
	conn, err := grpc.Dial(accessURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("could not connect to access api server: %w", err)
	}
	accessClient := access.NewAccessAPIClient(conn)

	// get the network's chainID
	resp, err := accessClient.GetNetworkParameters(ctx, &access.GetNetworkParametersRequest{})
	if err != nil {
		return nil, fmt.Errorf("could not get network parameters: %w", err)
	}
	return flow.ChainID(resp.ChainId).Chain(), nil
}
