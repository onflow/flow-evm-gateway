import EVM

access(all)
fun main(hexEncodedTx: String, hexEncodedAddress: String): EVM.Result {
    let address = EVM.addressFromString(hexEncodedAddress)

    return EVM.dryRun(tx: hexEncodedTx.decodeHex(), from: address)
}
