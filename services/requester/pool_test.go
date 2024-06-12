package requester

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// this test makes sure the template we use for sending evm transaction contains the
// evm error divider as it's used in the parsing of the cadence errors
func Test_RunTemplateDivider(t *testing.T) {
	require.True(t, strings.Contains(string(runTxScript), evmErrorDivider), "evm error divider not used in run cadence script")
}
