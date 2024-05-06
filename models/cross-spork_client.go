package models

import (
	"fmt"
	"github.com/onflow/flow-go-sdk/access"
	"math"
)

// CrossSporkClient is a wrapper around the Flow AN client that can
// access different AN APIs based on the height boundaries of the sporks.
//
// Each spork is defined with the last height included in that spork,
// based on the list we know which AN host to use when requesting the data.
type CrossSporkClient struct {
	// this map holds the last height and host for each spork
	sporkHosts map[uint64]string

	access.Client
}

// NewCrossSporkClient creates a new instance of the client, it accepts the
// host to the current spork AN API.
func NewCrossSporkClient(currentSporkHost string) (*CrossSporkClient, error) {
	c := &CrossSporkClient{
		sporkHosts: make(map[uint64]string),
	}
	// add current spork AN host with the highest value for last height
	err := c.AddSpork(math.MaxUint64, currentSporkHost)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *CrossSporkClient) AddSpork(lastHeight uint64, host string) error {
	if _, ok := c.sporkHosts[lastHeight]; ok {
		return fmt.Errorf("provided last height already exists")
	}

	c.sporkHosts[lastHeight] = host

	return nil
}
