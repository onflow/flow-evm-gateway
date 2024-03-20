import EVM

access(all)
fun main(hexEncodedTx: String): String {
    let account = getAuthAccount<auth(Storage) &Account>(Address(0xCOA))

    let coa = account.storage.borrow<&EVM.CadenceOwnedAccount>(
        from: /storage/evm
    ) ?? panic("Could not borrow reference to the COA!")
    let txResult = EVM.run(tx: hexEncodedTx.decodeHex(), coinbase: coa.address())

    return String.encodeHex(txResult.data)
}
