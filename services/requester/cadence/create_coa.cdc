import EVM
import FungibleToken
import FlowToken

transaction(amount: UFix64) {
    let auth: auth(Storage) &Account

    prepare(signer: auth(Storage) &Account) {
        self.auth = signer
    }

    execute {
        // If the COA is already created & saved, there's nothing to do, just return.
        if let coa = self.auth.storage.borrow<&EVM.CadenceOwnedAccount>(from: /storage/evm) {
            return
        }

        let vaultRef = self.auth.storage.borrow<auth(FungibleToken.Withdraw) &FlowToken.Vault>(
            from: /storage/flowTokenVault
        ) ?? panic("Could not borrow reference to the owner's Vault!")
        let vault <- vaultRef.withdraw(amount: amount) as! @FlowToken.Vault

        let account <- EVM.createCadenceOwnedAccount()
        account.deposit(from: <-vault)

        self.auth.storage.save<@EVM.CadenceOwnedAccount>(
            <-account,
            to: /storage/evm
        )
    }
}
