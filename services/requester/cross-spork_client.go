package requester

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/onflow/cadence"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"go.uber.org/ratelimit"
	"golang.org/x/exp/slices"
)

type sporkClient struct {
	firstHeight                    uint64
	lastHeight                     uint64
	client                         access.Client
	getEventsForHeightRangeLimiter ratelimit.Limiter
}

// contains checks if the provided height is withing the range of available heights
func (s *sporkClient) contains(height uint64) bool {
	return height >= s.firstHeight && height <= s.lastHeight
}

func (s *sporkClient) GetEventsForHeightRange(
	ctx context.Context, eventType string, startHeight uint64, endHeight uint64,
) ([]flow.BlockEvents, error) {
	s.getEventsForHeightRangeLimiter.Take()

	return s.client.GetEventsForHeightRange(ctx, eventType, startHeight, endHeight)
}

func (s *sporkClient) Close() error {
	return s.client.Close()
}

type sporkClients []*sporkClient

// addSpork will add a new spork host defined by the first and last height boundary in that spork.
func (s *sporkClients) add(logger zerolog.Logger, client access.Client) error {
	header, err := client.GetLatestBlockHeader(context.Background(), true)
	if err != nil {
		return fmt.Errorf("could not get latest height using the spork client: %w", err)
	}

	info, err := client.GetNodeVersionInfo(context.Background())
	if err != nil {
		return fmt.Errorf("could not get node info using the spork client: %w", err)
	}

	logger.Info().
		Uint64("firstHeight", info.NodeRootBlockHeight).
		Uint64("lastHeight", header.Height).
		Msg("adding spork client")

	*s = append(*s, &sporkClient{
		firstHeight: info.NodeRootBlockHeight,
		lastHeight:  header.Height,
		client:      client,
		// TODO (JanezP): Make this configurable
		getEventsForHeightRangeLimiter: ratelimit.New(100, ratelimit.WithoutSlack),
	})

	// make sure clients are always sorted
	slices.SortFunc(*s, func(a, b *sporkClient) int {
		return int(a.firstHeight) - int(b.firstHeight)
	})

	return nil
}

// get spork client that contains the height or nil if not found.
func (s *sporkClients) get(height uint64) access.Client {
	for _, spork := range *s {
		if spork.contains(height) {
			return spork.client
		}
	}

	return nil
}

// continuous checks if all the past spork clients create a continuous
// range of heights.
func (s *sporkClients) continuous() bool {
	for i := 0; i < len(*s)-1; i++ {
		if (*s)[i].lastHeight+1 != (*s)[i+1].firstHeight {
			return false
		}
	}

	return true
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
	sporkClients            sporkClients
	currentSporkFirstHeight uint64
	access.Client
}

// NewCrossSporkClient creates a new instance of the multi-spork client. It requires
// the current spork client and a slice of past spork clients.
func NewCrossSporkClient(
	currentSpork access.Client,
	pastSporks []access.Client,
	logger zerolog.Logger,
	chainID flowGo.ChainID,
) (*CrossSporkClient, error) {
	nodeRootBlockHeight := uint64(0)

	// Temp fix due to the fact that Emulator does not support the
	// GetNodeVersionInfo method.
	if chainID != flowGo.Emulator {
		info, err := currentSpork.GetNodeVersionInfo(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to get node version info: %w", err)
		}
		nodeRootBlockHeight = info.NodeRootBlockHeight
	}

	clients := sporkClients{}
	for _, c := range pastSporks {
		if err := clients.add(logger, c); err != nil {
			return nil, err
		}
	}

	if !clients.continuous() {
		return nil, fmt.Errorf("provided past-spork clients don't create a continuous range of heights")
	}

	return &CrossSporkClient{
		logger:                  logger,
		currentSporkFirstHeight: nodeRootBlockHeight,
		sporkClients:            clients,
		Client:                  currentSpork,
	}, nil
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

	client := c.sporkClients.get(height)
	if client == nil {
		return nil, errs.NewHeightOutOfRangeError(height)
	}

	c.logger.Debug().
		Uint64("requested-cadence-height", height).
		Msg("using previous spork client")

	return client, nil
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

func (c *CrossSporkClient) GetEventsForHeightRange(
	ctx context.Context, eventType string, startHeight uint64, endHeight uint64,
) ([]flow.BlockEvents, error) {
	client, err := c.getClientForHeight(startHeight)
	if err != nil {
		return nil, err
	}
	endClient, err := c.getClientForHeight(endHeight)
	if err != nil {
		return nil, err
	}
	// there is one client reference per spork, so we can compare the clients
	if endClient != client {
		return nil, fmt.Errorf("invalid height range, end height %d is not in the same spork as start height %d", endHeight, startHeight)
	}
	return client.GetEventsForHeightRange(ctx, eventType, startHeight, endHeight)
}

func (c *CrossSporkClient) Close() error {
	var merr *multierror.Error

	for _, client := range c.sporkClients {
		if err := client.Close(); err != nil {
			merr = multierror.Append(merr, err)
		}
	}
	err := c.Client.Close()
	if err != nil {
		merr = multierror.Append(merr, err)
	}

	return merr.ErrorOrNil()
}
