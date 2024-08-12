package metrics

import (
	"math/big"
	"time"

	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/go-ethereum/common"
)

type nopCollector struct{}

var _ Collector = &nopCollector{}

var NopCollector = &nopCollector{}

func (c *nopCollector) ApiErrorOccurred()                                 {}
func (c *nopCollector) TraceDownloadFailed()                              {}
func (c *nopCollector) ServerPanicked(string)                             {}
func (c *nopCollector) EVMHeightIndexed(uint64)                           {}
func (c *nopCollector) EVMAccountInteraction(string)                      {}
func (c *nopCollector) MeasureRequestDuration(time.Time, string)          {}
func (c *nopCollector) EVMFeesCollected(common.Address, uint64, *big.Int) {}
func (c *nopCollector) FlowFeesCollected(sdk.Address, []sdk.Event)        {}
