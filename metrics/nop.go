package metrics

import (
	"time"

	"github.com/onflow/flow-go-sdk"
)

type nopCollector struct{}

var _ Collector = &nopCollector{}

var NopCollector = &nopCollector{}

func (c *nopCollector) ApiErrorOccurred()                        {}
func (c *nopCollector) TraceDownloadFailed()                     {}
func (c *nopCollector) ServerPanicked(string)                    {}
func (c *nopCollector) CadenceHeightIndexed(uint64)              {}
func (c *nopCollector) EVMHeightIndexed(uint64)                  {}
func (c *nopCollector) EVMAccountInteraction(string)             {}
func (c *nopCollector) MeasureRequestDuration(time.Time, string) {}
func (c *nopCollector) OperatorBalance(*flow.Account)            {}
