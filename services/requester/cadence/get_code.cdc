import EVM

access(all)
fun main(hexEncodedAddress: String): String {
    let address = EVM.addressFromString(hexEncodedAddress)

    return String.encodeHex(address.code())
}
