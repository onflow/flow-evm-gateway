package integration

import (
	"context"
	"github.com/onflow/cadence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestIntegration_SimpleTransactions(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	blocks, _, _, err := startEventIngestionEngine(context.Background())
	require.NoError(t, err)

	emu := srv.Emulator()

	code := `
	transaction(amount: UFix64) {
		let fundVault: @FlowToken.Vault
	
		prepare(signer: auth(Storage) &Account) {
			let vaultRef = signer.storage.borrow<auth(FungibleToken.Withdraw) &FlowToken.Vault>(from: /storage/flowTokenVault)
				?? panic("Could not borrow reference to the owner's Vault!")
	
			self.fundVault <- vaultRef.withdraw(amount: amount) as! @FlowToken.Vault
		}
	
		execute {
			let acc <- EVM.createBridgedAccount()
			acc.deposit(from: <-self.fundVault)
			
			log(acc.balance())
			destroy acc
		}
	}`

	res, err := sendTransaction(emu, code, cadence.UFix64(4))
	require.NoError(t, err)

	assert.Len(t, res.Events, 4)

	time.Sleep(10 * time.Second)

	_, err = blocks.GetByHeight(2)
	require.NoError(t, err)
}
