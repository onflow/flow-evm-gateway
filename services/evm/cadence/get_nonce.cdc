import EVM

access(all)
fun main(hexEncodedAddress: String): UInt64 {
    let address = EVM.addressFromString(hexEncodedAddress)

    return address.nonce()
}
