package requester

import (
	"context"
	"errors"
	"fmt"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/rs/zerolog"
)

var ErrOutOfRange = errors.New("height is out of range for provided spork clients")

type sporkClient struct {
	firstHeight uint64
	lastHeight  uint64
	client      access.Client
}

// contains checks if the provided height is withing the range of available heights
func (s *sporkClient) contains(height uint64) bool {
	return height >= s.firstHeight && height <= s.lastHeight
}

// CrossSporkClient is a wrapper around the Flow AN client that can
// access different AN APIs based on the height boundaries of the sporks.
//
// Each spork is defined with the last height included in that spork,
// based on the list we know which AN client to use when requesting the data.
//
// Any API that supports cross-spork access must have a defined function
// that shadows the original access Client function.
type CrossSporkClient struct {
	logger                  zerolog.Logger
	sporkClients            []*sporkClient
	currentSporkFirstHeight uint64
	access.Client
}

// NewCrossSporkClient creates a new instance of the multi-spork client. It requires
// the current spork client and a slice of past spork clients.
func NewCrossSporkClient(
	currentSpork access.Client,
	pastSporks []access.Client,
	logger zerolog.Logger,
) (*CrossSporkClient, error) {
	info, err := currentSpork.GetNodeVersionInfo(context.Background())
	if err != nil {
		return nil, err
	}

	client := &CrossSporkClient{
		logger:                  logger,
		sporkClients:            make([]*sporkClient, 0),
		currentSporkFirstHeight: info.NodeRootBlockHeight,
		Client:                  currentSpork,
	}

	for _, sporkClient := range pastSporks {
		if err := client.addSpork(sporkClient); err != nil {
			return nil, err
		}
	}

	return client, nil
}

// addSpork will add a new spork host defined by the first and last height boundary in that spork.
func (c *CrossSporkClient) addSpork(client access.Client) error {
	header, err := client.GetLatestBlockHeader(context.Background(), true)
	if err != nil {
		return fmt.Errorf("could not get latest height using the spork client: %w", err)
	}

	info, err := client.GetNodeVersionInfo(context.Background())
	if err != nil {
		return fmt.Errorf("could not get node info using the spork client: %w", err)
	}

	spork := &sporkClient{
		firstHeight: info.NodeRootBlockHeight,
		lastHeight:  header.Height,
		client:      client,
	}

	c.sporkClients = append(c.sporkClients, spork)

	c.logger.Info().
		Uint64("spork-last-height", header.Height).
		Uint64("spork-first-height", info.NodeRootBlockHeight).
		Msg("added spork client")

	return nil
}

// IsPastSpork will check if the provided height is contained in the previous sporks.
func (c *CrossSporkClient) IsPastSpork(height uint64) bool {
	return height < c.currentSporkFirstHeight
}

// getClientForHeight returns the client for the given height that contains the height range.
//
// If the height is not contained in any of the past spork clients we return an error.
// If the height is contained in the current spork client we return the current spork client,
// but that doesn't guarantee the height will be found, since the height might be bigger than the
// latest height in the current spork, which is not checked due to performance reasons.
func (c *CrossSporkClient) getClientForHeight(height uint64) (access.Client, error) {
	if !c.IsPastSpork(height) {
		return c.Client, nil
	}

	for _, spork := range c.sporkClients {
		if spork.contains(height) {
			c.logger.Debug().
				Uint64("requested-cadence-height", height).
				Msg("using previous spork client")

			return spork.client, nil
		}
	}

	// if not found return an error
	return nil, ErrOutOfRange
}

// GetLatestHeightForSpork will determine the spork client in which the provided height is contained
// and then find the latest height in that spork.
func (c *CrossSporkClient) GetLatestHeightForSpork(ctx context.Context, height uint64) (uint64, error) {
	client, err := c.getClientForHeight(height)
	if err != nil {
		return 0, err
	}

	block, err := client.GetLatestBlockHeader(ctx, true)
	if err != nil {
		return 0, err
	}
	return block.Height, nil
}

func (c *CrossSporkClient) GetBlockHeaderByHeight(
	ctx context.Context,
	height uint64,
) (*flow.BlockHeader, error) {
	client, err := c.getClientForHeight(height)
	if err != nil {
		return nil, err
	}
	return client.GetBlockHeaderByHeight(ctx, height)
}

func (c *CrossSporkClient) ExecuteScriptAtBlockHeight(
	ctx context.Context,
	height uint64,
	script []byte,
	arguments []cadence.Value,
) (cadence.Value, error) {
	client, err := c.getClientForHeight(height)
	if err != nil {
		return nil, err
	}
	return client.ExecuteScriptAtBlockHeight(ctx, height, script, arguments)
}

func (c *CrossSporkClient) SubscribeEventsByBlockHeight(
	ctx context.Context,
	startHeight uint64,
	filter flow.EventFilter,
	opts ...access.SubscribeOption,
) (<-chan flow.BlockEvents, <-chan error, error) {
	client, err := c.getClientForHeight(startHeight)
	if err != nil {
		return nil, nil, err
	}
	return client.SubscribeEventsByBlockHeight(ctx, startHeight, filter, opts...)
}
