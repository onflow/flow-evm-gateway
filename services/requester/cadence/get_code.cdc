import EVM

access(all)
fun main(hexEncodedAddress: String): String {
    let addressBytes = hexEncodedAddress.decodeHex().toConstantSized<[UInt8; 20]>()!
    let address = EVM.EVMAddress(bytes: addressBytes)

    return String.encodeHex(address.code())
}
