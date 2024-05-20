package requester

import (
	"context"
	"fmt"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/rs/zerolog"
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
	logger zerolog.Logger
	// this map holds the last heights and clients for each spork
	sporkClients    map[uint64]access.Client
	sporkBoundaries []uint64
	access.Client
}

// NewCrossSporkClient creates a new instance of the multi-spork client. It requires
// the current spork client and a slice of past spork clients.
func NewCrossSporkClient(
	currentSpork access.Client,
	pastSporks []access.Client,
	logger zerolog.Logger,
) (*CrossSporkClient, error) {
	client := &CrossSporkClient{
		logger:       logger,
		sporkClients: make(map[uint64]access.Client),
		Client:       currentSpork,
	}

	for _, sporkClient := range pastSporks {
		if err := client.addSpork(sporkClient); err != nil {
			return nil, err
		}
	}

	// create a descending list of block heights that represent boundaries
	// of each spork, after crossing each height, we use a different client
	heights := maps.Keys(client.sporkClients)
	slices.Sort(heights)
	slices.Reverse(heights) // make it descending
	client.sporkBoundaries = heights

	return client, nil
}

// addSpork will add a new spork host defined by the last height boundary in that spork.
func (c *CrossSporkClient) addSpork(client access.Client) error {
	header, err := client.GetLatestBlockHeader(context.Background(), true)
	if err != nil {
		return fmt.Errorf("could not get latest height using the spork client: %w", err)
	}

	lastHeight := header.Height

	if _, ok := c.sporkClients[lastHeight]; ok {
		return fmt.Errorf("provided last height already exists")
	}

	c.sporkClients[lastHeight] = client

	c.logger.Info().
		Uint64("spork-boundary", lastHeight).
		Msg("added spork specific client")

	return nil
}

// IsPastSpork will check if the provided height is contained in the previous sporks.
func (c *CrossSporkClient) IsPastSpork(height uint64) bool {
	return len(c.sporkBoundaries) > 0 && height <= c.sporkBoundaries[0]
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

	// start by using the current spork client, then iterate all the upper height boundaries
	// and find the last client that still contains the height in its upper height limit
	client := c.Client
	for _, upperBound := range c.sporkBoundaries {
		if upperBound >= height {
			client = c.sporkClients[upperBound]

			c.logger.Debug().
				Uint64("spork-boundary", upperBound).
				Msg("using previous spork client")
		}
	}

	return client
}

// GetLatestHeightForSpork will determine the spork client in which the provided height is contained
// and then find the latest height in that spork.
func (c *CrossSporkClient) GetLatestHeightForSpork(ctx context.Context, height uint64) (uint64, error) {
	block, err := c.
		getClientForHeight(height).
		GetLatestBlockHeader(ctx, true)
	if err != nil {
		return 0, err
	}

	return block.Height, nil
}

func (c *CrossSporkClient) GetBlockHeaderByHeight(
	ctx context.Context,
	height uint64,
) (*flow.BlockHeader, error) {
	return c.
		getClientForHeight(height).
		GetBlockHeaderByHeight(ctx, height)
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
	opts ...access.SubscribeOption,
) (<-chan flow.BlockEvents, <-chan error, error) {
	return c.
		getClientForHeight(startHeight).
		SubscribeEventsByBlockHeight(ctx, startHeight, filter, opts...)
}
