import EVM

access(all)
fun main(hexEncodedTx: String): EVM.Result {
    let account = getAuthAccount<auth(Storage) &Account>(Address(0xCOA))

    let coa = account.storage.borrow<&EVM.CadenceOwnedAccount>(
        from: /storage/evm
    ) ?? panic("Could not borrow reference to the COA!")

    return EVM.run(tx: hexEncodedTx.decodeHex(), coinbase: coa.address())
}
