package models

import (
	"context"
	"fmt"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

// CrossSporkClient is a wrapper around the Flow AN client that can
// access different AN APIs based on the height boundaries of the sporks.
//
// Each spork is defined with the last height included in that spork,
// based on the list we know which AN client to use when requesting the data.
//
// Any API that supports cross-spork access must have a defined function
// that shadows the original access Client function.
type CrossSporkClient struct {
	// this map holds the last heights and clients for each spork
	sporkHosts map[uint64]access.Client

	access.Client
}

// NewCrossSporkClient creates a new instance of the client, it accepts the
// host to the current spork AN API.
func NewCrossSporkClient(currentSporkHost string) (*CrossSporkClient, error) {
	// add current spork AN host as the default client
	client, err := grpc.NewClient(currentSporkHost)
	if err != nil {
		return nil, err
	}

	return &CrossSporkClient{
		make(map[uint64]access.Client),
		client,
	}, nil
}

func (c *CrossSporkClient) AddSpork(lastHeight uint64, host string) error {
	if _, ok := c.sporkHosts[lastHeight]; ok {
		return fmt.Errorf("provided last height already exists")
	}

	client, err := grpc.NewClient(host)
	if err != nil {
		return err
	}

	c.sporkHosts[lastHeight] = client

	return nil
}

// getClientForHeight returns the client for the given height. It starts by using the current spork client,
// then iteratively checks the upper height boundaries in descending order and returns the last client
// that still contains the given height within its upper height limit. If no client is found, it returns
// the current spork client.
// Please note that even if a client for provided height is found we don't guarantee the data being available
// because it still might not have access to the height provided, because there might be other sporks with
// lower height boundaries that we didn't configure for.
// This would result in the error when using the client to access such data.
func (c *CrossSporkClient) getClientForHeight(height uint64) access.Client {
	heights := maps.Keys(c.sporkHosts)
	slices.Sort(heights)    // order heights in ascending order
	slices.Reverse(heights) // make it descending

	// start by using the current spork client, then iterate all the upper height boundaries
	// and find the last client that still contains the height in its upper height limit
	client := c.Client
	for _, upperBound := range heights {
		if upperBound > height {
			client = c.sporkHosts[upperBound]
		}
	}

	return client
}

func (c *CrossSporkClient) GetBlockByHeight(
	ctx context.Context,
	height uint64,
) (*flow.Block, error) {
	return c.
		getClientForHeight(height).
		GetBlockByHeight(ctx, height)
}

func (c *CrossSporkClient) ExecuteScriptAtBlockHeight(
	ctx context.Context,
	height uint64,
	script []byte,
	arguments []cadence.Value,
) (cadence.Value, error) {
	return c.
		getClientForHeight(height).
		ExecuteScriptAtBlockHeight(ctx, height, script, arguments)
}

func (c *CrossSporkClient) SubscribeEventsByBlockHeight(
	ctx context.Context,
	startHeight uint64,
	filter flow.EventFilter,
) (<-chan flow.BlockEvents, <-chan error, error) {
	return c.
		getClientForHeight(startHeight).
		SubscribeEventsByBlockHeight(ctx, startHeight, filter)
}
