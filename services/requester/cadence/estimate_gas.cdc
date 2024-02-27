import EVM

access(all)
fun main(encodedTx: [UInt8]): [UInt64; 2] {
    let account = getAuthAccount<auth(Storage) &Account>(Address(0xCOA))

    let coa = account.storage.borrow<&EVM.CadenceOwnedAccount>(
        from: /storage/evm
    ) ?? panic("Could not borrow reference to the COA!")
    let txResult = EVM.run(tx: encodedTx, coinbase: coa.address())

    return [txResult.errorCode, txResult.gasUsed]
}
