package integration

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIntegration_SimpleTransactions(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	emu := srv.Emulator()

	code := `
		transaction() {
			execute {
				let acc <- EVM.createBridgedAccount()
				destroy acc
			}
		}
	`
	require.NoError(t, sendTransaction(emu, code))
}
