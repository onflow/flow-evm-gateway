package requester

import (
	"context"
	"regexp"

	gethTypes "github.com/onflow/go-ethereum/core/types"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

const (
	evmErrorRegex = `evm_error=(.*)\n`
)

// TxPool is the minimum interface that needs to be implemented by
// the various transaction pool strategies.
type TxPool interface {
	Add(ctx context.Context, tx *gethTypes.Transaction) error
}

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
