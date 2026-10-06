import EVM
import FungibleToken
import FlowToken

transaction(hexEncodedTxs: [String]) {
    let coa: auth(EVM.Withdraw) &EVM.CadenceOwnedAccount

    prepare(signer: auth(Capabilities, Storage) &Account) {
        let vaultRef = signer.storage.borrow<&FlowToken.Vault>(
            from: /storage/flowTokenVault
        ) ?? panic("Could not borrow reference to the owner's Vault!")

        if !signer.storage.check<@EVM.CadenceOwnedAccount>(from: /storage/evm_coa) {
            signer.storage.save<@EVM.CadenceOwnedAccount>(
                <- EVM.createCadenceOwnedAccount(),
                to: /storage/evm_coa
            )
        }

        self.coa = signer.storage.borrow<auth(EVM.Withdraw) &EVM.CadenceOwnedAccount>(
            from: /storage/evm_coa
        )!

        // Whenever the COA address, that is used as coinbase, has accumulated
        // more than 5 FLOW from tx fees, move that amount to the configured
        // Flow account that submits Cadence transactions on behalf of the EVM
        // Gateway node operator.
        let coaBalance = self.coa.balance()
        if coaBalance.inFLOW() >= 5.0 {
            coaBalance.setFLOW(flow: 5.0)
            vaultRef.deposit(from: <-self.coa.withdraw(balance: coaBalance))
        }
    }

    execute {
        let txs: [[UInt8]] = []
        for tx in hexEncodedTxs {
            txs.append(tx.decodeHex())
        }

        // If there's only one tx, use `EVM.run`.
        // If there are more, then use `EVM.batchRun`
        if txs.length == 1 {
            let txResult = EVM.run(
                tx: txs[0],
                coinbase: self.coa.address()
            )
            assert(
                txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
                message: "evm_error=\(txResult.errorMessage);evm_error_code=\(txResult.errorCode)"
            )
            return
        }

        let txResults = EVM.batchRun(
            txs: txs,
            coinbase: self.coa.address()
        )

        // If at least one of the EVM transactions in the batch is either
        // failed or successful, in other words not invalid, we let the
        // Cadence transaction succeed.
        for txResult in txResults {
            if txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful {
                return
            }
        }

        // Otherwise, all EVM transactions are invalid txs and can't be
        // executed (such as nonce too low).
        // In this case, we fail the Cadence transaction with the error
        // message from the first EVM transaction.
        for txResult in txResults {
            assert(
                txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
                message: "evm_error=\(txResult.errorMessage);evm_error_code=\(txResult.errorCode)"
            )
        }
    }
}
