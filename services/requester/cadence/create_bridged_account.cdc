import EVM
import FungibleToken
import FlowToken

transaction(amount: UFix64) {
    let sentVault: @FlowToken.Vault
    let auth: auth(Storage) &Account

    prepare(signer: auth(Storage) &Account) {
        let vaultRef = signer.storage.borrow<auth(FungibleToken.Withdraw) &FlowToken.Vault>(
            from: /storage/flowTokenVault
        ) ?? panic("Could not borrow reference to the owner's Vault!")

        self.sentVault <- vaultRef.withdraw(amount: amount) as! @FlowToken.Vault
        self.auth = signer
    }

    execute {
        let account <- EVM.createBridgedAccount()
        log(account.address())
        account.deposit(from: <-self.sentVault)

        log(account.balance())
        self.auth.storage.save<@EVM.BridgedAccount>(<-account, to: StoragePath(identifier: "evm")!)
    }
}
