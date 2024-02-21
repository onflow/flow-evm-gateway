import EVM

transaction(encodedTx: [UInt8]) {
    let bridgedAccount: &EVM.BridgedAccount

    prepare(signer: auth(Storage) &Account) {
        self.bridgedAccount = signer.storage.borrow<&EVM.BridgedAccount>(
            from: /storage/evm
        ) ?? panic("Could not borrow reference to the bridged account!")
    }

    execute {
        EVM.run(tx: encodedTx, coinbase: self.bridgedAccount.address())
    }
}
