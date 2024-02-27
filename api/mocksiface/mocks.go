package mocksiface

import "github.com/onflow/flow-go-sdk/access"

// This is a proxy for the real access.Client for mockery to use.
type FlowAccessClient interface {
	access.Client
}
