// TODO(m-Peter): Use proper address for each network
import EVM from 0xf8d6e0586b0a20c7

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
