import EVM

access(all)
fun main(hexEncodedAddress: String, key: String): [UInt8] {
    let addressBytes = hexEncodedAddress.decodeHex().toConstantSized<[UInt8; 20]>()!
    let address = EVM.EVMAddress(bytes: addressBytes)

    return address.storageAt(key: key.decodeHex())
}
