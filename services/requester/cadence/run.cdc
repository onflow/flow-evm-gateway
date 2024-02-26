import EVM

transaction(encodedTx: [UInt8]) {
    let coa: &EVM.CadenceOwnedAccount

    prepare(signer: auth(Storage) &Account) {
        self.coa = signer.storage.borrow<&EVM.CadenceOwnedAccount>(from: /storage/evm)
            ?? panic("Could not borrow reference to the bridged account!")
    }

    execute {
        let result = EVM.run(tx: encodedTx, coinbase: self.coa.address())
        // todo only temporary until we correctly handle failure events
        assert(
            result.status == EVM.Status.successful,
            message: "failed to execute evm transaction: ".concat(result.errorCode.toString())
        )
    }
}
