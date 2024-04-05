package tests

import (
	"testing"
)

func Test_Web3Acceptance(t *testing.T) {
	runWeb3Test(t, "eth_non-interactive_test")
}
