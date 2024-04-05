package tests

import (
	"testing"
)

func TestWeb3_E2E(t *testing.T) {
	t.Run("non-interactive eth APIs tests", func(t *testing.T) {
		runWeb3Test(t, "eth_non-interactive_test")
	})

	t.Run("deploy contract and call methods", func(t *testing.T) {
		runWeb3Test(t, "eth_deploy_contract_and_interact_test")
	})

	t.Run("transfer Flow between EOA accounts", func(t *testing.T) {
		runWeb3Test(t, "eth_transfer_between_eoa_accounts_test")
	})
}
