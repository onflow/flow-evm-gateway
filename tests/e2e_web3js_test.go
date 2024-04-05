package tests

import (
	"testing"
)

func Test_NonInteractiveTest(t *testing.T) {
	runWeb3Test(t, "eth_non-interactive_test")
}

func Test_DeployContract(t *testing.T) {
	runWeb3Test(t, "eth_deploy_test")
}
