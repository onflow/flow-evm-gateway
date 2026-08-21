package requester

import (
	"context"
	"regexp"
	"time"

	gethTypes "github.com/ethereum/go-ethereum/core/types"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

const evmErrorRegex = `evm_error=(.*);`

// TxPool is the minimum interface that needs to be implemented by
// the various transaction pool strategies.
type TxPool interface {
	Add(ctx context.Context, tx *gethTypes.Transaction) error
}

// fastPathSubmitTimeout bounds how long a single fast-path submission may take.
// The fast path holds the pool-wide mutex across the whole Flow submission, so
// without a bound a hung Access-node call would block every other EOA's Add and
// the background flush loop for as long as the caller's context allows — up to
// RpcRequestTimeout (120s by default). This is a liveness safety ceiling, NOT a
// latency SLA: normal submits complete well within it and release the lock
// immediately; only a genuinely stalled call is cut off (and its tx is
// dropped-and-logged for the client to resubmit).
const fastPathSubmitTimeout = 10 * time.Second

// flushReason values label why a batch was submitted, for the submission log
// and the txpool_submissions_total metric.
const (
	flushReasonFastPath = "fast-path"
	flushReasonPrefix   = "consecutive-prefix"
)

// this will extract the evm specific error from the Flow transaction error message
// the run.cdc script panics with the evm specific error as the message which we
// extract and return to the client. Any error returned that is evm specific
// is a validation error due to assert statement in the run.cdc script.
func parseInvalidError(err error) (error, bool) {
	r := regexp.MustCompile(evmErrorRegex)
	matches := r.FindStringSubmatch(err.Error())
	if len(matches) != 2 {
		return nil, false
	}

	return errs.NewFailedTransactionError(matches[1]), true
}
