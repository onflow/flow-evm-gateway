import EVM

transaction(hexEncodedTx: String) {
    let coa: &EVM.CadenceOwnedAccount

    prepare(signer: auth(Storage) &Account) {
        self.coa = signer.storage.borrow<&EVM.CadenceOwnedAccount>(
            from: /storage/evm
        ) ?? panic("Could not borrow reference to the COA!")
    }

    execute {
        let txResult = EVM.run(
            tx: hexEncodedTx.decodeHex(),
            coinbase: self.coa.address()
        )
        assert(
            txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
            message: "failed to execute evm transaction: ".concat(txResult.errorCode.toString())
        )
    }
}
