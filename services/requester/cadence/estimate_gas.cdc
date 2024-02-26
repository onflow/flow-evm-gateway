import EVM

access(all)
fun main(encodedTx: [UInt8]): [UInt64; 2] {
    let coa <- EVM.createCadenceOwnedAccount()
    let txResult = EVM.run(tx: encodedTx, coinbase: coa.address())

    destroy <- coa

    return [txResult.errorCode, txResult.gasUsed]
}
