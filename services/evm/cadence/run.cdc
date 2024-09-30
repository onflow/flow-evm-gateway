import EVM

transaction(hexEncodedTx: String, coinbase: String) {
    let coa: &EVM.CadenceOwnedAccount

    prepare(signer: auth(Storage) &Account) {
        self.coa = signer.storage.borrow<&EVM.CadenceOwnedAccount>(
            from: /storage/evm
        ) ?? panic("Could not borrow reference to the COA!")
    }

    execute {
        let txResult = EVM.run(
            tx: hexEncodedTx.decodeHex(),
            coinbase: EVM.addressFromString(coinbase)
        )
        assert(
            txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
            message: "evm_error=".concat(txResult.errorMessage).concat("\n")
        )
    }
}
